export namespace main {
	
	export class startInfo {
	    mode: string;
	    command?: string[];
	
	    static createFrom(source: any = {}) {
	        return new startInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.command = source["command"];
	    }
	}

}

