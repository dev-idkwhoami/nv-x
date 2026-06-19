export namespace config {

	export class CameraConfig {
	    InputDevice: string;
	    InputFormat: string;
	    Width: number;
	    Height: number;
	    FPS: number;

	    static createFrom(source: any = {}) {
	        return new CameraConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.InputDevice = source["InputDevice"];
	        this.InputFormat = source["InputFormat"];
	        this.Width = source["Width"];
	        this.Height = source["Height"];
	        this.FPS = source["FPS"];
	    }
	}
	export class UIConfig {
	    Theme: string;

	    static createFrom(source: any = {}) {
	        return new UIConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Theme = source["Theme"];
	    }
	}
	export class ServiceConfig {
	    Name: string;
	    ExecPath: string;

	    static createFrom(source: any = {}) {
	        return new ServiceConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.ExecPath = source["ExecPath"];
	    }
	}
	export class FXConfig {
	    Enabled: boolean;
	    Mode: string;
	    BackgroundImage: string;
	    ChromaColor: string;
	    SDKPath: string;
	    ModelDir: string;
	    EnableOSReleaseShim: boolean;
	    BlurStrength: number;
	    DenoiseEnabled: boolean;
	    DenoiseStrength: number;

	    static createFrom(source: any = {}) {
	        return new FXConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Enabled = source["Enabled"];
	        this.Mode = source["Mode"];
	        this.BackgroundImage = source["BackgroundImage"];
	        this.ChromaColor = source["ChromaColor"];
	        this.SDKPath = source["SDKPath"];
	        this.ModelDir = source["ModelDir"];
	        this.EnableOSReleaseShim = source["EnableOSReleaseShim"];
	        this.BlurStrength = source["BlurStrength"];
	        this.DenoiseEnabled = source["DenoiseEnabled"];
	        this.DenoiseStrength = source["DenoiseStrength"];
	    }
	}
	export class LoopbackConfig {
	    ConfigPath: string;
	    ExclusiveCaps: boolean;
	    MaxBuffers: number;

	    static createFrom(source: any = {}) {
	        return new LoopbackConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ConfigPath = source["ConfigPath"];
	        this.ExclusiveCaps = source["ExclusiveCaps"];
	        this.MaxBuffers = source["MaxBuffers"];
	    }
	}
	export class OutputConfig {
	    Device: string;
	    VideoNR: number;
	    Label: string;
	    OutputFormat: string;

	    static createFrom(source: any = {}) {
	        return new OutputConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Device = source["Device"];
	        this.VideoNR = source["VideoNR"];
	        this.Label = source["Label"];
	        this.OutputFormat = source["OutputFormat"];
	    }
	}
	export class Config {
	    Camera: CameraConfig;
	    Output: OutputConfig;
	    Loopback: LoopbackConfig;
	    FX: FXConfig;
	    Service: ServiceConfig;
	    UI: UIConfig;

	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Camera = this.convertValues(source["Camera"], CameraConfig);
	        this.Output = this.convertValues(source["Output"], OutputConfig);
	        this.Loopback = this.convertValues(source["Loopback"], LoopbackConfig);
	        this.FX = this.convertValues(source["FX"], FXConfig);
	        this.Service = this.convertValues(source["Service"], ServiceConfig);
	        this.UI = this.convertValues(source["UI"], UIConfig);
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





}

export namespace devices {

	export class Device {
	    SysName: string;
	    Path: string;
	    Name: string;

	    static createFrom(source: any = {}) {
	        return new Device(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.SysName = source["SysName"];
	        this.Path = source["Path"];
	        this.Name = source["Name"];
	    }
	}

}

export namespace fx {

	export class Snapshot {
	    state: string;
	    device: string;
	    dependencies: string[];
	    consumers: number;
	    message: string;
	    updatedAt: string;

	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.device = source["device"];
	        this.dependencies = source["dependencies"];
	        this.consumers = source["consumers"];
	        this.message = source["message"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

export namespace loopback {

	export class FoundConfig {
	    Path: string;
	    Content: string;
	    IsNV: boolean;

	    static createFrom(source: any = {}) {
	        return new FoundConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.Content = source["Content"];
	        this.IsNV = source["IsNV"];
	    }
	}

}

export namespace main {

	export class ActionResult {
	    ok: boolean;
	    message: string;
	    output: string;

	    static createFrom(source: any = {}) {
	        return new ActionResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.message = source["message"];
	        this.output = source["output"];
	    }
	}
	export class ServiceView {
	    name: string;
	    exists: boolean;
	    active: boolean;
	    error: string;
	    output: string;

	    static createFrom(source: any = {}) {
	        return new ServiceView(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.exists = source["exists"];
	        this.active = source["active"];
	        this.error = source["error"];
	        this.output = source["output"];
	    }
	}
	export class AppStatus {
	    devices: devices.Device[];
	    v4l2LoopbackLoaded: boolean;
	    loopbackConfigExists: boolean;
	    loopbackConfigPath: string;
	    service: ServiceView;
	    expectedInput: string;
	    expectedInputExists: boolean;
	    expectedOutput: string;
	    expectedOutputExists: boolean;
	    configRendered: string;
	    fx: fx.Snapshot;

	    static createFrom(source: any = {}) {
	        return new AppStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.devices = this.convertValues(source["devices"], devices.Device);
	        this.v4l2LoopbackLoaded = source["v4l2LoopbackLoaded"];
	        this.loopbackConfigExists = source["loopbackConfigExists"];
	        this.loopbackConfigPath = source["loopbackConfigPath"];
	        this.service = this.convertValues(source["service"], ServiceView);
	        this.expectedInput = source["expectedInput"];
	        this.expectedInputExists = source["expectedInputExists"];
	        this.expectedOutput = source["expectedOutput"];
	        this.expectedOutputExists = source["expectedOutputExists"];
	        this.configRendered = source["configRendered"];
	        this.fx = this.convertValues(source["fx"], fx.Snapshot);
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
	export class ConfigView {
	    path: string;
	    found: boolean;
	    rendered: string;
	    config: config.Config;

	    static createFrom(source: any = {}) {
	        return new ConfigView(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.found = source["found"];
	        this.rendered = source["rendered"];
	        this.config = this.convertValues(source["config"], config.Config);
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
	export class LoopbackView {
	    targetPath: string;
	    found: loopback.FoundConfig[];
	    conflict: boolean;
	    warning: string;
	    rendered: string;

	    static createFrom(source: any = {}) {
	        return new LoopbackView(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetPath = source["targetPath"];
	        this.found = this.convertValues(source["found"], loopback.FoundConfig);
	        this.conflict = source["conflict"];
	        this.warning = source["warning"];
	        this.rendered = source["rendered"];
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

	export class ThemeView {
	    theme: string;

	    static createFrom(source: any = {}) {
	        return new ThemeView(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	    }
	}

}
