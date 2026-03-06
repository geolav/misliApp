package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "GOApp/proto/user/v1" // Убедись, что путь правильный!
)

type Server struct {
	grpcClient pb.UserServiceClient
}

type ApiResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type PostRequest struct {
	Content  string `json:"content"`
	MediaUrl string `json:"media_url"`
}

type CommentRequest struct {
	PostId  string `json:"post_id"`
	Content string `json:"content"`
}

type SearchRequest struct {
	Username string `json:"username"`
}

func main() {
	// Подключаемся к твоему gRPC серверу
	conn, err := grpc.Dial("localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		log.Fatal("❌ Failed to connect to gRPC server:", err)
	}
	defer conn.Close()

	grpcClient := pb.NewUserServiceClient(conn)
	s := &Server{grpcClient: grpcClient}

	// Настраиваем маршруты
	mux := http.NewServeMux()

	// Раздаём статические файлы из папки ../web
	fs := http.FileServer(http.Dir("../web"))
	mux.Handle("/", fs)

	// API endpoints
	mux.HandleFunc("/api/feed", s.handleFeed)
	mux.HandleFunc("/api/profile", s.handleProfile)
	mux.HandleFunc("/api/user/search", s.handleUserSearch)
	mux.HandleFunc("/api/post/create", s.handleCreatePost)
	mux.HandleFunc("/api/post/delete", s.handleDeletePost)
	mux.HandleFunc("/api/post/comment", s.handleAddComment)
	mux.HandleFunc("/api/post/comments", s.handleGetComments)

	log.Println("🌐 Mini App HTTP server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// Вспомогательная функция для получения tg_id
func getTgID(r *http.Request) string {
	// 1. Из заголовка (Telegram Mini App)
	if tgID := r.Header.Get("X-Telegram-User-Id"); tgID != "" {
		return tgID
	}
	// 2. Из query-параметра (для тестирования в браузере)
	return r.URL.Query().Get("tg_id")
}

// Создаёт gRPC контекст с metadata
func (s *Server) grpcContext(ctx context.Context, tgID string) context.Context {
	md := metadata.New(map[string]string{"tg_id": tgID})
	return metadata.NewOutgoingContext(ctx, md)
}

// ================== API HANDLERS ==================

// GET /api/post/comments?post_id=...
func (s *Server) handleGetComments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tgID := getTgID(r)
	postID := r.URL.Query().Get("post_id")

	if tgID == "" || postID == "" {
		http.Error(w, `{"success":false,"message":"missing tg_id or post_id"}`, http.StatusBadRequest)
		return
	}

	ctx := s.grpcContext(r.Context(), tgID)

	comments, err := s.grpcClient.GetComments(ctx, &pb.GetCommentsRequest{
		PostId:   postID,
		Page:     1,
		PageSize: 50,
	})

	if err != nil {
		http.Error(w, `{"success":false,"message":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{
		Success: true,
		Data:    comments,
	})
}

// GET /api/feed?tg_id=123
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tgID := getTgID(r)
	if tgID == "" {
		http.Error(w, `{"success":false,"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	ctx := s.grpcContext(r.Context(), tgID)

	// Получаем пользователя по tg_id
	user, err := s.grpcClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{TgId: tgID})
	if err != nil {
		http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
		return
	}

	// Получаем ленту (пагинацию можно добавить позже)
	feed, err := s.grpcClient.GetFeed(ctx, &pb.GetFeedRequest{
		UserId:   user.UserId,
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		http.Error(w, `{"success":false,"message":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{Success: true, Data: feed})
}

// GET /api/profile?tg_id=123
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tgID := getTgID(r)
	if tgID == "" {
		http.Error(w, `{"success":false,"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	ctx := s.grpcContext(r.Context(), tgID)

	user, err := s.grpcClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{TgId: tgID})
	if err != nil {
		http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{Success: true, Data: user})
}

// GET /api/user/search?username=efnms&tg_id=123
func (s *Server) handleUserSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tgID := getTgID(r)
	username := r.URL.Query().Get("username")

	if tgID == "" || username == "" {
		http.Error(w, `{"success":false,"message":"missing tg_id or username"}`, http.StatusBadRequest)
		return
	}

	ctx := s.grpcContext(r.Context(), tgID)

	user, err := s.grpcClient.GetUserByUsername(ctx, &pb.GetUserRequest{Username: username})
	if err != nil {
		http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{Success: true, Data: user})
}

// POST /api/post/create
// Body: {"content": "...", "media_url": "..."}
func (s *Server) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tgID := getTgID(r)
	if tgID == "" {
		http.Error(w, `{"success":false,"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req PostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"success":false,"message":"invalid json"}`, http.StatusBadRequest)
		return
	}

	ctx := s.grpcContext(r.Context(), tgID)

	post, err := s.grpcClient.CreatePost(ctx, &pb.CreatePostRequest{
		TgId:     tgID,
		Content:  req.Content,
		MediaUrl: req.MediaUrl,
	})
	if err != nil {
		http.Error(w, `{"success":false,"message":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{Success: true, Data: post})
}

// POST /api/post/delete?post_id=...
func (s *Server) handleDeletePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tgID := getTgID(r)
	postID := r.URL.Query().Get("post_id")

	if tgID == "" || postID == "" {
		http.Error(w, `{"success":false,"message":"missing tg_id or post_id"}`, http.StatusBadRequest)
		return
	}

	ctx := s.grpcContext(r.Context(), tgID)

	// Сначала получаем пользователя, чтобы узнать его user_id
	user, err := s.grpcClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{TgId: tgID})
	if err != nil {
		http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
		return
	}

	resp, err := s.grpcClient.DeletePost(ctx, &pb.DeletePostRequest{
		PostId: postID,
		UserId: user.UserId,
	})
	if err != nil {
		http.Error(w, `{"success":false,"message":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{Success: resp.Success, Message: resp.Message})
}

// POST /api/post/comment
// Body: {"post_id": "...", "content": "..."}
func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tgID := getTgID(r)
	if tgID == "" {
		http.Error(w, `{"success":false,"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req CommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"success":false,"message":"invalid json"}`, http.StatusBadRequest)
		return
	}

	ctx := s.grpcContext(r.Context(), tgID)

	comment, err := s.grpcClient.AddComment(ctx, &pb.AddCommentRequest{
		PostId:  req.PostId,
		TgId:    tgID,
		Content: req.Content,
	})
	if err != nil {
		http.Error(w, `{"success":false,"message":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{Success: true, Data: comment})
}
