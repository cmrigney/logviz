package main

import (
	"bufio"
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

const channelBuffer = 8192

func main() {
	lines := make(chan LogLine, channelBuffer)
	info, wrapArgs := parseMode(os.Args[1:])

	app := NewApp(lines, info)

	switch info.Mode {
	case "pipe":
		go readStream(os.Stdin, os.Stdout, "stdin", app, nil)
	case "wrap":
		go runWrapped(wrapArgs, app)
	}

	err := wails.Run(&options.App{
		Title:  "logviz",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 20, G: 22, B: 28, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		fmt.Fprintln(os.Stderr, "[logviz] error:", err)
	}
}

func parseMode(args []string) (startInfo, []string) {
	for i, a := range args {
		if a == "--" {
			return startInfo{Mode: "wrap", Command: args[i+1:]}, args[i+1:]
		}
	}
	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		return startInfo{Mode: "pipe"}, nil
	}
	return startInfo{Mode: "idle"}, nil
}

// readStream copies src → passthrough line-by-line while also pushing each line
// into the app. When done is non-nil it is closed after the reader finishes.
func readStream(src io.Reader, passthrough io.Writer, source string, app *App, done chan struct{}) {
	if done != nil {
		defer close(done)
	}
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		text := scanner.Text()
		fmt.Fprintln(passthrough, text)
		app.push(source, text)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "[logviz]", source, "read error:", err)
	}
}

func runWrapped(cmdArgs []string, app *App) {
	if len(cmdArgs) == 0 {
		fmt.Fprintln(os.Stderr, "[logviz] wrap mode requires a command after --")
		return
	}
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "[logviz] stdout pipe:", err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "[logviz] stderr pipe:", err)
		return
	}
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "[logviz] start:", err)
		return
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		if sig, ok := <-sigCh; ok {
			_ = cmd.Process.Signal(sig)
		}
	}()

	outDone := make(chan struct{})
	errDone := make(chan struct{})
	go readStream(stdout, os.Stdout, "stdout", app, outDone)
	go readStream(stderr, os.Stderr, "stderr", app, errDone)

	<-outDone
	<-errDone
	_ = cmd.Wait()
	signal.Stop(sigCh)
	close(sigCh)
}
