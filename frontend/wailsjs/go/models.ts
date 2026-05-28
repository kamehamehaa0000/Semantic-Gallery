export namespace main {
	
	export class ImageResult {
	    id: number;
	    path: string;
	    thumb_path: string;
	
	    static createFrom(source: any = {}) {
	        return new ImageResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.thumb_path = source["thumb_path"];
	    }
	}

}

