import os
import sys
import grpc
import torch
import open_clip
import faiss
import numpy as np
from PIL import Image
from concurrent import futures
import logging

sys.path.append(os.path.join(os.path.dirname(__file__), 'proto', 'pb'))

from proto.pb import gallery_pb2
from proto.pb  import gallery_pb2_grpc

# Add the generated pb files to the path

logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)

class GalleryService(gallery_pb2_grpc.GalleryServiceServicer):
    def __init__(self, model_name="ViT-B-32", pretrained="laion2b_s34b_b79k", data_dir="data"):
        self.device = "cuda" if torch.cuda.is_available() else "cpu"
        logger.info(f"Using device: {self.device}")
        
        # Load model
        logger.info(f"Loading model {model_name}...")
        self.model, _, self.preprocess = open_clip.create_model_and_transforms(model_name, pretrained=pretrained, device=self.device)
        self.tokenizer = open_clip.get_tokenizer(model_name)
        self.model.eval()
        
        # Setup data directory and FAISS index
        self.data_dir = data_dir
        os.makedirs(self.data_dir, exist_ok=True)
        self.index_path = os.path.join(self.data_dir, "gallery.index")
        self.ids_path = os.path.join(self.data_dir, "ids.npy")
        
        # Embedding dimension
        self.dim = 512 # For ViT-B-32
        
        if os.path.exists(self.index_path):
            logger.info("Loading existing FAISS index...")
            self.index = faiss.read_index(self.index_path)
            self.ids = np.load(self.ids_path).tolist()
        else:
            logger.info("Creating new FAISS index...")
            self.index = faiss.IndexFlatIP(self.dim) # Inner product for cosine similarity on normalized vectors
            self.ids = []

    def save_index(self):
        faiss.write_index(self.index, self.index_path)
        np.save(self.ids_path, np.array(self.ids))

    def IndexImages(self, request, context):
        logger.info(f"Indexing {len(request.entries)} images...")
        new_vectors = []
        new_ids = []
        
        for entry in request.entries:
            try:
                image = self.preprocess(Image.open(entry.path)).unsqueeze(0).to(self.device)
                with torch.no_grad():
                    image_features = self.model.encode_image(image)
                    image_features /= image_features.norm(dim=-1, keepdim=True)
                    
                new_vectors.append(image_features.cpu().numpy()[0])
                new_ids.append(entry.id)
            except Exception as e:
                logger.error(f"Failed to index {entry.path}: {e}")
                continue
        
        if new_vectors:
            self.index.add(np.array(new_vectors).astype('float32'))
            self.ids.extend(new_ids)
            self.save_index()
            return gallery_pb2.IndexResponse(success=True, message=f"Indexed {len(new_vectors)} images")
        
        return gallery_pb2.IndexResponse(success=False, message="No images were indexed")

    def Search(self, request, context):
        logger.info(f"Searching for: {request.query}")
        try:
            text = self.tokenizer([request.query]).to(self.device)
            with torch.no_grad():
                text_features = self.model.encode_text(text)
                text_features /= text_features.norm(dim=-1, keepdim=True)
            
            query_vector = text_features.cpu().numpy().astype('float32')
            
            limit = min(request.limit, len(self.ids)) if request.limit > 0 else len(self.ids)
            if limit == 0:
                return gallery_pb2.SearchResponse(ids=[], scores=[])
                
            scores, indices = self.index.search(query_vector, limit)
            
            result_ids = [self.ids[idx] for idx in indices[0] if idx != -1]
            result_scores = [float(score) for score in scores[0] if indices[0][0] != -1] # Simple check if results exist
            
            return gallery_pb2.SearchResponse(ids=result_ids, scores=result_scores)
        except Exception as e:
            logger.error(f"Search failed: {e}")
            return gallery_pb2.SearchResponse(ids=[], scores=[])

    def DeleteImages(self, request, context):
        ids_to_remove = set(request.ids)
        if not ids_to_remove:
            return gallery_pb2.DeleteResponse(success=True)
            
        logger.info(f"Deleting {len(ids_to_remove)} images from index...")
        
        try:
            # Reconstruct the index to remove entries
            # 1. Get all vectors
            if len(self.ids) == 0:
                return gallery_pb2.DeleteResponse(success=True)
                
            all_vectors = faiss.rev_swig_ptr(self.index.get_xb(), self.index.ntotal * self.dim)
            all_vectors = all_vectors.reshape(self.index.ntotal, self.dim)
            
            # 2. Identify indices to keep
            keep_indices = [i for i, img_id in enumerate(self.ids) if img_id not in ids_to_remove]
            
            if len(keep_indices) == self.index.ntotal:
                return gallery_pb2.DeleteResponse(success=True)
                
            # 3. Create new index and add kept vectors
            new_index = faiss.IndexFlatIP(self.dim)
            if keep_indices:
                keep_vectors = all_vectors[keep_indices].copy().astype('float32')
                new_index.add(keep_vectors)
                
            self.index = new_index
            self.ids = [self.ids[i] for i in keep_indices]
            
            self.save_index()
            logger.info(f"Deleted {len(ids_to_remove)} images. New total: {len(self.ids)}")
            return gallery_pb2.DeleteResponse(success=True)
        except Exception as e:
            logger.error(f"DeleteImages failed: {e}")
            return gallery_pb2.DeleteResponse(success=False)

    def HealthCheck(self, request, context):
        return gallery_pb2.HealthResponse(healthy=True)

def serve():
    data_dir = os.environ.get("GALLERY_DATA_DIR", "data")
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    gallery_pb2_grpc.add_GalleryServiceServicer_to_server(GalleryService(data_dir=data_dir), server)
    server.add_insecure_port('[::]:50051')
    logger.info("Starting gRPC server on port 50051...")
    server.start()
    logger.info("gRPC server started.")
    server.wait_for_termination()
    logger.info("gRPC server stopped.")


if __name__ == '__main__':
    serve()
