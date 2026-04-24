export namespace main {

	export class ConfigField {
	    type: string;
	    default: string;
	    description: string;

	    static createFrom(source: any = {}) {
	        return new ConfigField(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.default = source["default"];
	        this.description = source["description"];
	    }
	}

	export class PluginInfo {
	    name: string;
	    enabled: boolean;
	    running: boolean;
	    config: {[key: string]: string};
	    configSchema?: {[key: string]: ConfigField};
	    description?: string;
	    protocol: number;

	    static createFrom(source: any = {}) {
	        return new PluginInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.running = source["running"];
	        this.config = source["config"];
	        this.configSchema = source["configSchema"];
	        this.description = source["description"];
	        this.protocol = source["protocol"];
	    }
	}

	export class startInfo {
	    mode: string;
	    command?: string[];
	    passthrough: boolean;

	    static createFrom(source: any = {}) {
	        return new startInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.command = source["command"];
	        this.passthrough = source["passthrough"];
	    }
	}

}
