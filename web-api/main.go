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
	"google.golang.org/grpc/status"

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

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
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
	mux.HandleFunc("/api/user/posts", s.handleUserPosts)
	mux.HandleFunc("/api/post/get", s.handleGetPost)
	mux.HandleFunc("/api/profile", s.handleProfile)
	mux.HandleFunc("/api/user/search", s.handleUserSearch)
	mux.HandleFunc("/api/post/create", s.handleCreatePost)
	mux.HandleFunc("/api/post/delete", s.handleDeletePost)
	mux.HandleFunc("/api/post/comment", s.handleAddComment)
	mux.HandleFunc("/api/post/comments", s.handleGetComments)
	mux.HandleFunc("/api/user/subscribe", s.handleSubscribe)
	mux.HandleFunc("/api/user/unsubscribe", s.handleUnsubscribe)
	mux.HandleFunc("/api/auth/login", s.handleLoginOrRegister)
	mux.HandleFunc("/api/user/delete", s.handleDeleteUser)

	log.Println("🌐 Mini App HTTP server starting on :8080")
	//log.Fatal(http.ListenAndServe(":8080", mux))
	log.Fatal(http.ListenAndServe(":8080", corsMiddleware(mux)))
}

// Вспомогательная функция для получения tg_id
func getTgID(r *http.Request) string {
	// 1. Из заголовка (Telegram Mini App)
	if tgID := r.URL.Query().Get("tg_id"); tgID != "" {
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

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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
		user2, err2 := s.grpcClient.GetUserByID(ctx, &pb.GetUserByIDRequest{UserId: tgID})
		if err2 != nil {
			http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
			return
		}
		user = user2
	}
	_, err = s.grpcClient.DeleteUser(ctx, &pb.DeleteUserRequest{UserId: user.UserId})
	if err != nil {
		http.Error(w, `{"success":false,"message":"failed to delete"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{Success: true, Message: "account deleted"})
}

// POST /api/user/subscribe
func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tgID := getTgID(r)
	if tgID == "" {
		http.Error(w, `{"success":false,"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var body struct {
		UserId string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"success":false,"message":"invalid json"}`, http.StatusBadRequest)
		return
	}
	ctx := s.grpcContext(r.Context(), tgID)
	currentUser, err := s.grpcClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{TgId: tgID})
	if err != nil {
		currentUser, err = s.grpcClient.GetUserByID(ctx, &pb.GetUserByIDRequest{UserId: tgID})
		if err != nil {
			http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
			return
		}
	}
	resp, err := s.grpcClient.Subscribe(ctx, &pb.SubscribeRequest{
		FollowerId:  currentUser.UserId,
		FollowingId: body.UserId,
	})
	if err != nil {
		http.Error(w, `{"success":false,"message":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{Success: resp.Success, Message: resp.Message})
}

// POST /api/user/unsubscribe
func (s *Server) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tgID := getTgID(r)
	if tgID == "" {
		http.Error(w, `{"success":false,"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var body struct {
		UserId string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"success":false,"message":"invalid json"}`, http.StatusBadRequest)
		return
	}
	ctx := s.grpcContext(r.Context(), tgID)
	currentUser, err := s.grpcClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{TgId: tgID})
	if err != nil {
		currentUser, err = s.grpcClient.GetUserByID(ctx, &pb.GetUserByIDRequest{UserId: tgID})
		if err != nil {
			http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
			return
		}
	}
	resp, err := s.grpcClient.Unsubscribe(ctx, &pb.UnsubscribeRequest{
		FollowerId:  currentUser.UserId,
		FollowingId: body.UserId,
	})
	if err != nil {
		http.Error(w, `{"success":false,"message":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{Success: resp.Success, Message: resp.Message})
}

func (s *Server) handleLoginOrRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"success":false,"message":"invalid json"}`, http.StatusBadRequest)
		return
	}

	resp, err := s.grpcClient.LoginOrRegister(r.Context(), &pb.LoginOrRegisterRequest{
		Username: body.Username,
		Password: body.Password,
		Name:     body.Name,
	})
	if err != nil {
		st, _ := status.FromError(err)
		http.Error(w, `{"success":false,"message":"`+st.Message()+`"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{
		Success: true,
		Data: map[string]interface{}{
			"token":    resp.Token,
			"user_id":  resp.UserId,
			"username": resp.Username,
			"name":     resp.Name,
			"is_new":   resp.IsNew,
		},
	})
}

func (s *Server) handleGetPost(w http.ResponseWriter, r *http.Request) {

	tgID := getTgID(r)
	postID := r.URL.Query().Get("post_id")
	ctx := s.grpcContext(r.Context(), tgID)
	post, err := s.grpcClient.GetPost(ctx, &pb.GetPostRequest{
		PostId: postID,
	})

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(ApiResponse{
		Success: true,
		Data:    post,
	})
}

func (s *Server) handleUserPosts(w http.ResponseWriter, r *http.Request) {
	tgID := r.URL.Query().Get("tg_id")
	ctx := s.grpcContext(r.Context(), tgID)
	user, err := s.grpcClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{TgId: tgID})
	if err != nil {
		user, err = s.grpcClient.GetUserByID(ctx, &pb.GetUserByIDRequest{UserId: tgID})
		if err != nil {
			http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
			return
		}
	}
	posts, err := s.grpcClient.GetUserPosts(ctx, &pb.GetUserPostsRequest{
		TgId:     user.TgId,
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(ApiResponse{Success: true, Data: posts})
}

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

	postWithComments, err := s.grpcClient.GetPost(ctx, &pb.GetPostRequest{
		PostId: postID,
	})

	if err != nil {
		log.Println("❌ GetComments error:", err)
		http.Error(w, `{"success":false,"message":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{
		Success: true,
		Data:    postWithComments.Comments,
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
	user, err := s.grpcClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{TgId: tgID})
	if err != nil {
		user, err = s.grpcClient.GetUserByID(ctx, &pb.GetUserByIDRequest{UserId: tgID})
		if err != nil {
			http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
			return
		}
	}
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
		user, err = s.grpcClient.GetUserByID(ctx, &pb.GetUserByIDRequest{UserId: tgID})
		if err != nil {
			http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
			return
		}
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
	//user, err := s.grpcClient.GetUserByUsername(ctx, &pb.GetUserRequest{Username: username})
	md := metadata.Pairs("tg_id", tgID)
	ctx := metadata.NewOutgoingContext(context.Background(), md)

	user, err := s.grpcClient.GetUserByUsername(
		ctx,
		&pb.GetUserRequest{
			Username: username,
		},
	)
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
	// Получаем настоящий tg_id если передан user_id
	user, err := s.grpcClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{TgId: tgID})
	if err != nil {
		user, err = s.grpcClient.GetUserByID(ctx, &pb.GetUserByIDRequest{UserId: tgID})
		if err != nil {
			http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
			return
		}
	}
	post, err := s.grpcClient.CreatePost(ctx, &pb.CreatePostRequest{
		TgId:     user.TgId,
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
	user, err := s.grpcClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{TgId: tgID})
	if err != nil {
		user, err = s.grpcClient.GetUserByID(ctx, &pb.GetUserByIDRequest{UserId: tgID})
		if err != nil {
			http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
			return
		}
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
	user, err := s.grpcClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{TgId: tgID})
	if err != nil {
		user, err = s.grpcClient.GetUserByID(ctx, &pb.GetUserByIDRequest{UserId: tgID})
		if err != nil {
			http.Error(w, `{"success":false,"message":"user not found"}`, http.StatusNotFound)
			return
		}
	}
	comment, err := s.grpcClient.AddComment(ctx, &pb.AddCommentRequest{
		PostId:  req.PostId,
		TgId:    user.TgId,
		Content: req.Content,
	})
	if err != nil {
		http.Error(w, `{"success":false,"message":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApiResponse{Success: true, Data: comment})
}
