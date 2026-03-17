package main

import (
	"GOApp/internal/database"
	pb "GOApp/proto/user/v1"
	"context"
	"database/sql"
	"log"
	"net"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"google.golang.org/grpc/metadata"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const jwtSecret = "secret92374893263295230289!@#@!$!214()#)&*#"

type Server struct {
	pb.UnimplementedUserServiceServer
	database *database.Database
}

func (s *Server) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.UserResponse, error) {
	if req.Name == "" || req.Username == "" {
		return nil, status.Error(codes.InvalidArgument, "name or username is empty")
	}

	user := &database.UserDB{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Username:  req.Username,
		Age:       req.Age,
		CreatedAt: time.Now().Format(time.DateTime),
	}

	if err := s.database.SaveUser(user); err != nil {
		log.Printf("Database error: %v", err)
		if strings.Contains(err.Error(), "duplicate key") ||
			strings.Contains(err.Error(), "unique constraint") {
			return nil, status.Error(codes.AlreadyExists, "user already exists")
		}
		return nil, status.Error(codes.Internal, "failed to save user")
	}

	return &pb.UserResponse{
		UserId:    user.ID,
		Name:      user.Name,
		Username:  user.Username,
		Age:       user.Age,
		CreatedAt: user.CreatedAt,
	}, nil
}

func (s *Server) GetUserByUsername(ctx context.Context, req *pb.GetUserRequest) (*pb.UserResponse, error) {
	if req.Username == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is empty")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	var viewerID string
	if ok {
		tgIDs := md.Get("tg_id")
		if len(tgIDs) > 0 {
			viewerTgID := tgIDs[0]

			viewer, err := s.database.GetUserByTgID(viewerTgID)
			if err == nil {
				viewerID = viewer.ID
			}
		}
	}

	user, err := s.database.GetUserByUsername(req.Username)
	var isSubscribed bool
	if viewerID != "" {
		isSubscribed, _ = s.database.IsSubscribed(viewerID, user.ID)
	}

	// Получаем дополнительную статистику (можно добавить методы в database)
	postsCount, _ := s.database.GetUserPostsCount(user.ID)
	followersCount, _ := s.database.GetFollowersCount(user.ID)
	followingCount, _ := s.database.GetFollowingCount(user.ID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return &pb.UserResponse{
		UserId:         user.ID,
		Name:           user.Name,
		Username:       user.Username,
		Age:            user.Age,
		CreatedAt:      user.CreatedAt,
		TgId:           user.TgID,
		Bio:            user.Bio,
		AvatarUrl:      user.AvatarURL,
		PostsCount:     postsCount,
		FollowersCount: followersCount,
		FollowingCount: followingCount,
		IsSubscribed:   isSubscribed,
	}, nil
}

func (s *Server) GetUserByID(ctx context.Context, req *pb.GetUserByIDRequest) (*pb.UserResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	user, err := s.database.GetUserByID(req.UserId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	postsCount, _ := s.database.GetUserPostsCount(user.ID)
	followersCount, _ := s.database.GetFollowersCount(user.ID)
	followingCount, _ := s.database.GetFollowingCount(user.ID)
	return &pb.UserResponse{
		UserId:         user.ID,
		Name:           user.Name,
		Username:       user.Username,
		Age:            user.Age,
		CreatedAt:      user.CreatedAt,
		TgId:           user.TgID,
		Bio:            user.Bio,
		AvatarUrl:      user.AvatarURL,
		PostsCount:     postsCount,
		FollowersCount: followersCount,
		FollowingCount: followingCount,
	}, nil
}

func (s *Server) LoginOrRegister(ctx context.Context, req *pb.LoginOrRegisterRequest) (*pb.LoginOrRegisterResponse, error) {
	if req.Username == "" || req.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "username and password are required")
	}

	var userID string
	var isNew bool

	existing, err := s.database.GetUserByUsername(req.Username)
	if err != nil {
		// Пользователь не найден — регистрируем
		if req.Name == "" {
			req.Name = req.Username
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to hash password")
		}

		newUser := &database.UserDB{
			ID:           uuid.New().String(),
			TgID:         "web_" + uuid.New().String(), // заглушка для tg_id
			Username:     req.Username,
			Name:         req.Name,
			PasswordHash: string(hash),
			CreatedAt:    time.Now().Format(time.DateTime),
			UpdatedAt:    time.Now().Format(time.DateTime),
		}

		if err := s.database.SaveUser(newUser); err != nil {
			if strings.Contains(err.Error(), "duplicate key") ||
				strings.Contains(err.Error(), "unique constraint") {
				return nil, status.Error(codes.AlreadyExists, "username already taken")
			}
			return nil, status.Error(codes.Internal, "failed to save user")
		}

		userID = newUser.ID
		isNew = true
	} else {
		if existing.PasswordHash == "" {
			// Первый вход с веба — устанавливаем пароль
			hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
			if err != nil {
				return nil, status.Error(codes.Internal, "failed to hash password")
			}
			if err := s.database.SetPassword(existing.ID, string(hash)); err != nil {
				return nil, status.Error(codes.Internal, "failed to set password")
			}
		} else {
			// Пароль уже есть — проверяем
			if err := bcrypt.CompareHashAndPassword([]byte(existing.PasswordHash), []byte(req.Password)); err != nil {
				return nil, status.Error(codes.PermissionDenied, "invalid password")
			}
		}
		userID = existing.ID
		isNew = false
	}

	// Генерируем JWT
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":  userID,
		"username": req.Username,
		"exp":      time.Now().Add(30 * 24 * time.Hour).Unix(),
	})

	tokenStr, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate token")
	}

	name := req.Username
	if !isNew && existing != nil {
		name = existing.Name
	}

	return &pb.LoginOrRegisterResponse{
		Token:    tokenStr,
		UserId:   userID,
		Username: req.Username,
		Name:     name,
		IsNew:    isNew,
	}, nil
}

func (s *Server) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
	if req.Token == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}

	token, err := jwt.Parse(req.Token, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, status.Error(codes.Unauthenticated, "invalid signing method")
		}
		return []byte(jwtSecret), nil
	})

	if err != nil || !token.Valid {
		return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "invalid token claims")
	}

	userID, _ := claims["user_id"].(string)
	username, _ := claims["username"].(string)

	user, err := s.database.GetUserByID(userID)
	tgID := ""
	if err == nil {
		tgID = user.TgID
	}

	return &pb.ValidateTokenResponse{
		UserId:   userID,
		Username: username,
		TgId:     tgID,
	}, nil
}

func (s *Server) GetUserByTgID(ctx context.Context, req *pb.GetUserByTgIDRequest) (*pb.UserResponse, error) {
	if req.TgId == "" {
		return nil, status.Error(codes.InvalidArgument, "tg_id is empty")
	}
	user, err := s.database.GetUserByTgID(req.TgId)

	// Получаем дополнительную статистику (можно добавить методы в database)
	postsCount, _ := s.database.GetUserPostsCount(user.ID)
	followersCount, _ := s.database.GetFollowersCount(user.ID)
	followingCount, _ := s.database.GetFollowingCount(user.ID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return &pb.UserResponse{
		UserId:         user.ID,
		Name:           user.Name,
		Username:       user.Username,
		Age:            user.Age,
		CreatedAt:      user.CreatedAt,
		TgId:           user.TgID,
		Bio:            user.Bio,
		AvatarUrl:      user.AvatarURL,
		PostsCount:     postsCount,
		FollowersCount: followersCount,
		FollowingCount: followingCount,
	}, nil
}

func (s *Server) RegisterUser(ctx context.Context, req *pb.RegisterUserRequest) (*pb.UserResponse, error) {
	if req.Name == "" || req.Username == "" {
		return nil, status.Error(codes.InvalidArgument, "name or username is empty")
	}

	if req.TgId == "" || req.Username == "" || req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "tg_id, username and name are required")
	}

	// Проверяем, не занят ли username
	existingUser, err := s.database.GetUserByUsername(req.Username)
	if err == nil && existingUser != nil {
		return nil, status.Error(codes.AlreadyExists, "username already taken")
	}

	// Проверяем, не зарегистрирован ли уже этот Telegram ID
	existingByTg, err := s.database.GetUserByTgID(req.TgId)
	if err == nil && existingByTg != nil {
		return nil, status.Error(codes.AlreadyExists, "user already registered with this Telegram ID")
	}
	//if err := s.database.SaveUser(user); err != nil {
	//	log.Printf("Database error: %v", err)
	//	if strings.Contains(err.Error(), "duplicate key") ||
	//		strings.Contains(err.Error(), "unique constraint") {
	//		return nil, status.Error(codes.AlreadyExists, "user already exists")
	//	}
	//	return nil, status.Error(codes.Internal, "failed to save user")
	//}

	user := &database.UserDB{
		ID:        uuid.New().String(),
		TgID:      req.TgId,
		Name:      req.Name,
		Username:  req.Username,
		Age:       req.Age,
		Bio:       req.Bio,
		AvatarURL: req.AvatarUrl,
		UpdatedAt: time.Now().Format(time.DateTime),
		CreatedAt: time.Now().Format(time.DateTime),
	}

	if err := s.database.SaveUser(user); err != nil {
		log.Printf("Database error: %v", err)
		if strings.Contains(err.Error(), "duplicate key") ||
			strings.Contains(err.Error(), "unique constraint") {
			return nil, status.Error(codes.AlreadyExists, "user already exists")
		}
		return nil, status.Error(codes.Internal, "failed to save user")
	}

	return &pb.UserResponse{
		UserId:    user.ID,
		Name:      user.Name,
		Username:  user.Username,
		Age:       user.Age,
		CreatedAt: user.CreatedAt,
		Bio:       user.Bio,
		AvatarUrl: user.AvatarURL,
	}, nil
}

func (s *Server) CreatePost(ctx context.Context, req *pb.CreatePostRequest) (*pb.PostResponse, error) {
	if req.TgId == "" || req.Content == "" {
		return nil, status.Error(codes.InvalidArgument, "tg_id and content are required")
	}
	// Получаем информацию об авторе
	user, err := s.database.GetUserByTgID(req.TgId)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get author info")
	}

	post := &database.PostDB{
		ID:        uuid.New().String(),
		AuthorID:  user.ID, // ← вот здесь используется внутренний ID пользователя
		Content:   req.Content,
		MediaURL:  req.MediaUrl,
		CreatedAt: time.Now().Format(time.RFC3339),
		UpdatedAt: time.Now().Format(time.RFC3339),
	}

	if err := s.database.CreatePost(post); err != nil {
		return nil, status.Error(codes.Internal, "failed to create post")
	}

	return &pb.PostResponse{
		PostId:         post.ID,
		AuthorTgId:     user.TgID,
		AuthorName:     user.Name,
		AuthorUsername: user.Username,
		Content:        post.Content,
		MediaUrl:       post.MediaURL,
		CreatedAt:      post.CreatedAt,
		UpdatedAt:      post.UpdatedAt,
	}, nil
}

func (s *Server) GetUserPosts(ctx context.Context, req *pb.GetUserPostsRequest) (*pb.PostsResponse, error) {
	if req.TgId == "" {
		return nil, status.Error(codes.InvalidArgument, "tg_id is required")
	}

	user, err := s.database.GetUserByTgID(req.TgId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	page := req.GetPage()
	if page < 1 {
		page = 1
	}
	pageSize := req.GetPageSize()
	if pageSize < 1 || pageSize > 50 {
		pageSize = 10
	}

	offset := (page - 1) * pageSize

	posts, err := s.database.GetUserPosts(user.ID, int(pageSize), int(offset))
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get posts")
	}

	var pbPosts []*pb.PostResponse
	for _, post := range posts {
		comments, err := s.database.GetPostComments(post.ID)
		commentsCount := int32(0)
		if err == nil {
			commentsCount = int32(len(comments))
		} else {
			log.Printf("⚠️ Failed to get comments for post %s: %v", post.ID, err)
		}
		pbPosts = append(pbPosts, &pb.PostResponse{
			PostId:         post.ID,
			AuthorTgId:     user.TgID, // tg_id один для всех постов
			AuthorName:     user.Name,
			AuthorUsername: user.Username,
			Content:        post.Content,
			MediaUrl:       post.MediaURL,
			CreatedAt:      post.CreatedAt,
			UpdatedAt:      post.UpdatedAt,
			CommentsCount:  commentsCount,
		})
	}
	totalCount, err := s.database.GetUserPostsCount(user.ID)
	if err != nil {
		log.Printf("⚠️ Failed to get total posts count: %v", err)
	}

	return &pb.PostsResponse{
		Posts:      pbPosts,
		TotalCount: totalCount,
	}, nil
}

func (s *Server) IsSubscribed(ctx context.Context, req *pb.IsSubscribedRequest) (*pb.IsSubscribedResponse, error) {
	if req.FollowerId == "" || req.FollowingId == "" {
		return nil, status.Error(codes.InvalidArgument, "follower_id and following_id are required")
	}

	isSubscribed, err := s.database.IsSubscribed(req.FollowerId, req.FollowingId)
	if err != nil {
		log.Printf("❌ Failed to check subscription: %v", err)
		return nil, status.Error(codes.Internal, "failed to check subscription")
	}

	return &pb.IsSubscribedResponse{
		IsSubscribed: isSubscribed,
	}, nil
}

func (s *Server) GetPost(ctx context.Context, req *pb.GetPostRequest) (*pb.PostWithCommentsResponse, error) {
	log.Printf("📖 GetPost called: postID=%s", req.PostId)

	if req.PostId == "" {
		return nil, status.Error(codes.InvalidArgument, "post_id is required")
	}

	// Получаем пост из базы данных
	post, err := s.database.GetPostByID(req.PostId)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("❌ Post not found: %s", req.PostId)
			return nil, status.Error(codes.NotFound, "post not found")
		}
		log.Printf("❌ Database error: %v", err)
		return nil, status.Error(codes.Internal, "failed to get post")
	}

	// Получаем автора поста
	author, err := s.database.GetUserByID(post.AuthorID)
	if err != nil {
		log.Printf("❌ Failed to get author: %v", err)
		return nil, status.Error(codes.Internal, "failed to get author info")
	}

	// Получаем комментарии к посту
	comments, err := s.database.GetPostComments(req.PostId)
	if err != nil {
		log.Printf("❌ Failed to get comments: %v", err)
		return nil, status.Error(codes.Internal, "failed to get comments")
	}

	// Формируем ответ с постом
	postResponse := &pb.PostResponse{
		PostId:         post.ID,
		AuthorTgId:     author.TgID,
		AuthorName:     author.Name,
		AuthorUsername: author.Username,
		Content:        post.Content,
		MediaUrl:       post.MediaURL,
		CreatedAt:      post.CreatedAt,
		UpdatedAt:      post.UpdatedAt,
		CommentsCount:  int32(len(comments)),
	}

	// Формируем ответ с комментариями
	var commentsResponse []*pb.CommentResponse
	for _, comment := range comments {
		// Получаем автора комментария
		commentAuthor, err := s.database.GetUserByID(comment.UserID)
		if err != nil {
			log.Printf("⚠️ Failed to get comment author: %v", err)
			continue
		}

		commentsResponse = append(commentsResponse, &pb.CommentResponse{
			CommentId:      comment.ID,
			PostId:         comment.PostID,
			AuthorTgId:     commentAuthor.TgID,
			AuthorName:     commentAuthor.Name,
			AuthorUsername: commentAuthor.Username,
			Content:        comment.Content,
			CreatedAt:      comment.CreatedAt,
		})
	}

	log.Printf("✅ Post %s retrieved with %d comments", req.PostId, len(commentsResponse))

	return &pb.PostWithCommentsResponse{
		Post:     postResponse,
		Comments: commentsResponse,
	}, nil
}

func (s *Server) DeletePost(ctx context.Context, req *pb.DeletePostRequest) (*pb.DeleteResponse, error) {
	log.Printf("🗑️ DeletePost called: postID=%s, userID=%s", req.PostId, req.UserId)

	if req.PostId == "" {
		return &pb.DeleteResponse{
			Success: false,
			Message: "post_id is required",
		}, status.Error(codes.InvalidArgument, "post_id is required")
	}

	if req.UserId == "" {
		return &pb.DeleteResponse{
			Success: false,
			Message: "user_id is required",
		}, status.Error(codes.InvalidArgument, "user_id is required")
	}

	// Получаем пост из базы данных
	post, err := s.database.GetPostByID(req.PostId)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("❌ Post not found: %s", req.PostId)
			return &pb.DeleteResponse{
				Success: false,
				Message: "post not found",
			}, status.Error(codes.NotFound, "post not found")
		}
		log.Printf("❌ Database error: %v", err)
		return &pb.DeleteResponse{
			Success: false,
			Message: "database error",
		}, status.Error(codes.Internal, "database error")
	}

	// Получаем информацию о пользователе, который пытается удалить пост
	user, err := s.database.GetUserByID(req.UserId)
	if err != nil {
		log.Printf("❌ User not found: %s", req.UserId)
		return &pb.DeleteResponse{
			Success: false,
			Message: "user not found",
		}, status.Error(codes.NotFound, "user not found")
	}

	// Проверяем права на удаление
	// 1. Является ли пользователь автором поста
	isAuthor := (post.AuthorID == req.UserId)

	// 2. Является ли пользователь администратором
	isAdmin, _ := s.database.IsAdmin(user.TgID)

	if !isAuthor && !isAdmin {
		log.Printf("⛔ Permission denied: user %s is not author or admin", req.UserId)
		return &pb.DeleteResponse{
			Success: false,
			Message: "you don't have permission to delete this post",
		}, status.Error(codes.PermissionDenied, "you don't have permission to delete this post")
	}

	// Удаляем пост
	err = s.database.DeletePost(req.PostId)
	if err != nil {
		log.Printf("❌ Failed to delete post: %v", err)
		return &pb.DeleteResponse{
			Success: false,
			Message: "failed to delete post: " + err.Error(),
		}, status.Error(codes.Internal, "failed to delete post")
	}

	log.Printf("✅ Post %s deleted successfully by user %s (author=%v, admin=%v)",
		req.PostId, req.UserId, isAuthor, isAdmin)

	return &pb.DeleteResponse{
		Success: true,
		Message: "post deleted successfully",
	}, nil
}

func (s *Server) AddComment(ctx context.Context, req *pb.AddCommentRequest) (*pb.CommentResponse, error) {
	if req.PostId == "" || req.TgId == "" || req.Content == "" {
		return nil, status.Error(codes.InvalidArgument, "post_id, user_id and content are required")
	}

	user, err := s.database.GetUserByTgID(req.TgId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	comment := &database.CommentDB{
		ID:        uuid.New().String(),
		PostID:    req.PostId,
		UserID:    user.ID,
		Content:   req.Content,
		CreatedAt: time.Now().Format(time.DateTime),
	}

	if err := s.database.AddComment(comment); err != nil {
		log.Printf("Database error: %v", err)
		return nil, status.Error(codes.Internal, "failed to add comment")
	}

	return &pb.CommentResponse{
		CommentId:      comment.ID,
		PostId:         comment.PostID,
		AuthorTgId:     user.TgID,
		AuthorName:     user.Name,
		AuthorUsername: user.Username,
		Content:        comment.Content,
		CreatedAt:      comment.CreatedAt,
	}, nil
}

func (s *Server) Subscribe(ctx context.Context, req *pb.SubscribeRequest) (*pb.SubscribeResponse, error) {
	if req.FollowerId == "" || req.FollowingId == "" {
		return nil, status.Error(codes.InvalidArgument, "follower_id and following_id are required")
	}

	//if req.FollowerId == req.FollowingId {
	//	return &pb.SubscribeResponse{
	//		Success: false,
	//		Message: "cannot subscribe to yourself",
	//	}, nil
	//}

	err := s.database.Subscribe(req.FollowerId, req.FollowingId)
	if err != nil {
		log.Printf("❌ Subscribe DB error: follower=%s, following=%s, err=%v",
			req.FollowerId, req.FollowingId, err) // ← вот этого не хватает!
		if strings.Contains(err.Error(), "duplicate key") {
			return &pb.SubscribeResponse{
				Success: false,
				Message: "already subscribed",
			}, nil
		}
		return nil, status.Error(codes.Internal, "failed to subscribe")
	}

	return &pb.SubscribeResponse{
		Success: true,
		Message: "subscribed successfully",
	}, nil
}

func (s *Server) Unsubscribe(ctx context.Context, req *pb.UnsubscribeRequest) (*pb.SubscribeResponse, error) {
	log.Printf("📌 SERVER Unsubscribe called: follower_id=%s, following_id=%s", req.FollowerId, req.FollowingId)

	if req.FollowerId == "" || req.FollowingId == "" {
		return &pb.SubscribeResponse{
			Success: false,
			Message: "follower_id and following_id are required",
		}, status.Error(codes.InvalidArgument, "follower_id and following_id are required")
	}

	err := s.database.Unsubscribe(req.FollowerId, req.FollowingId)
	if err != nil {
		log.Printf("❌ Database error: %v", err)
		if strings.Contains(err.Error(), "not found") {
			return &pb.SubscribeResponse{
				Success: false,
				Message: "subscription not found",
			}, nil
		}
		return nil, status.Error(codes.Internal, "failed to unsubscribe")
	}

	log.Printf("✅ Unsubscribe successful")
	return &pb.SubscribeResponse{
		Success: true,
		Message: "unsubscribed successfully",
	}, nil
}

func (s *Server) GetFeed(ctx context.Context, req *pb.GetFeedRequest) (*pb.PostsResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	page := req.GetPage()
	if page < 1 {
		page = 1
	}
	pageSize := req.GetPageSize()
	if pageSize < 1 || pageSize > 50 {
		pageSize = 10
	}

	offset := (page - 1) * pageSize

	posts, err := s.database.GetFeed(req.UserId, int(pageSize), int(offset))
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get feed")
	}

	var pbPosts []*pb.PostResponse
	for _, post := range posts {
		author, err := s.database.GetUserByID(post.AuthorID)
		if err != nil {
			log.Printf("Warning: author not found for post %s: %v", post.ID, err)
			continue
		}
		comments, err := s.database.GetPostComments(post.ID)
		commentsCount := int32(0)
		if err == nil {
			commentsCount = int32(len(comments))
		} else {
			log.Printf("⚠️ Failed to get comments for post %s: %v", post.ID, err)
		}

		log.Printf("Post %s comments count: %d", post.ID, commentsCount)
		pbPosts = append(pbPosts, &pb.PostResponse{
			PostId:         post.ID,
			AuthorTgId:     author.TgID,
			AuthorName:     author.Name,
			AuthorUsername: author.Username,
			Content:        post.Content,
			MediaUrl:       post.MediaURL,
			CreatedAt:      post.CreatedAt,
			UpdatedAt:      post.UpdatedAt,
			CommentsCount:  commentsCount,
		})
	}

	return &pb.PostsResponse{
		Posts: pbPosts,
	}, nil
}

func (s *Server) IsAdmin(ctx context.Context, req *pb.IsAdminRequest) (*pb.IsAdminResponse, error) {
	isAdmin, err := s.database.IsAdmin(req.TgID)
	if err != nil {
		return nil, status.Error(codes.Internal, "database error")
	}
	return &pb.IsAdminResponse{IsAdmin: isAdmin}, nil
}

func (s *Server) ListUsers(ctx context.Context, req *pb.ListUsersRequest) (*pb.ListUserResponse, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no metadata")
	}

	tgIDs := md.Get("tg_id")
	if len(tgIDs) == 0 {
		return nil, status.Error(codes.Unauthenticated, "tg_id not provided")
	}

	tgID := tgIDs[0]

	isAdmin, err := s.database.IsAdmin(tgID)
	if err != nil || !isAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin access required")
	}
	page := req.GetPage()
	if page < 1 {
		page = 1
	}
	pageSize := req.GetPageSize()
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}

	offset := (page - 1) * pageSize
	log.Printf("📊 ListUsers called: page=%d, pageSize=%d, offset=%d", page, pageSize, offset)

	users, err := s.database.GetAllUsers(int(pageSize), int(offset))
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get users")
	}

	log.Printf("📊 Got %d users from DB", len(users))

	var pbUsers []*pb.UserResponse
	for _, user := range users {
		pbUsers = append(pbUsers, &pb.UserResponse{
			UserId:    user.ID,
			Name:      user.Name,
			Username:  user.Username,
			Age:       user.Age,
			CreatedAt: user.CreatedAt,
		})
	}
	return &pb.ListUserResponse{
		User:       pbUsers,
		TotalCount: int32(len(pbUsers)), // В реальности нужно общее количество
	}, nil
}

func (s *Server) AddAdmin(ctx context.Context, req *pb.AddAdminRequest) (*pb.AddAdminResponse, error) {
	if req.TgID == "" {
		return &pb.AddAdminResponse{
			Ok:      false,
			Message: "tg_id is empty",
		}, status.Error(codes.InvalidArgument, "tg_id is empty")
	}
	if req.AddedBy != "" {
		isAdmin, err := s.database.IsAdmin(req.AddedBy)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to check admin rights")
		}
		if !isAdmin && req.AddedBy != "system" {
			return &pb.AddAdminResponse{
				Ok:      false,
				Message: "only admins can add new admins",
			}, status.Error(codes.PermissionDenied, "only admins can add new admins")
		}
	}

	isAdmin, err := s.database.IsAdmin(req.TgID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to check admin rights")
	}
	if isAdmin {
		return &pb.AddAdminResponse{
			Ok:      false,
			Message: "user is already an admin",
		}, nil
	}

	err = s.database.AddAdmin(req.TgID, req.AddedBy)
	if err != nil {
		return &pb.AddAdminResponse{
			Ok:      false,
			Message: "failed to add admin: " + err.Error(),
		}, status.Error(codes.Internal, "failed to add admin")
	}

	return &pb.AddAdminResponse{
		Ok:      true,
		Message: "admin added successfully",
	}, nil
}

func (s *Server) RemoveAdmin(ctx context.Context, req *pb.RemoveAdminRequest) (*pb.RemoveAdminResponse, error) {
	if req.TgID == "" {
		return &pb.RemoveAdminResponse{
			Ok:      false,
			Message: "telegram_id is required",
		}, status.Error(codes.InvalidArgument, "telegram_id is required")
	}

	// TODO Здесь тоже нужно проверить права вызывающего
	// Для простоты будем считать, что проверка происходит на уровне бота

	// Удаляем админа (нужно добавить метод в db.go)
	err := s.database.RemoveAdmin(req.TgID)
	if err != nil {
		return &pb.RemoveAdminResponse{
			Ok:      false,
			Message: "failed to remove admin: " + err.Error(),
		}, status.Error(codes.Internal, "database error")
	}

	return &pb.RemoveAdminResponse{
		Ok:      true,
		Message: "admin removed successfully",
	}, nil
}

func main() {
	log.Println("🚀 Server starting...")
	dbConn, err := database.NewDatabase("postgres://postgres:pass@localhost:5432/postgres?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer dbConn.Close() //  Wrap ??
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterUserServiceServer(s, &Server{database: dbConn})
	log.Println("server listening on localhost:50051")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
