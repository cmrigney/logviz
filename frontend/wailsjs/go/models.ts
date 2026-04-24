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
	    id: string;
	    name: string;
	    enabled: boolean;
	    running: boolean;
	    config: Record<string, string>;
	    configSchema?: Record<string, ConfigField>;
	    description?: string;
	    protocol: number;
	
	    static createFrom(source: any = {}) {
	        return new PluginInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.running = source["running"];
	        this.config = source["config"];
	        this.configSchema = this.convertValues(source["configSchema"], ConfigField, true);
	        this.description = source["description"];
	        this.protocol = source["protocol"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
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

