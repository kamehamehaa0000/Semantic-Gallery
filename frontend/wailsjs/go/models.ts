export namespace main {
	
	export class ImageResult {
	    id: number;
	    path: string;
	    hash: string;
	    folder_path: string;
	    filename: string;
	    extension: string;
	    file_size: number;
	    width: number;
	    height: number;
	    created_at: number;
	    last_modified: number;
	    thumb_path: string;
	
	    static createFrom(source: any = {}) {
	        return new ImageResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.hash = source["hash"];
	        this.folder_path = source["folder_path"];
	        this.filename = source["filename"];
	        this.extension = source["extension"];
	        this.file_size = source["file_size"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.created_at = source["created_at"];
	        this.last_modified = source["last_modified"];
	        this.thumb_path = source["thumb_path"];
	    }
	}

}

