package database

import (
	"database/sql"
	"fmt"
)

type UserDB struct {
	ID        string
	TgID      string
	Username  string
	Name      string
	Bio       string
	Age       int32
	AvatarURL string
	CreatedAt string
	UpdatedAt string
}

type PostDB struct {
	ID        string
	AuthorID  string
	Content   string
	MediaURL  string
	CreatedAt string
	UpdatedAt string
}

type CommentDB struct {
	ID        string
	PostID    string
	UserID    string
	Content   string
	CreatedAt string
}

type SubscriptionDB struct {
	FollowerID  string
	FollowingID string
	CreatedAt   string
}

type AdminDB struct {
	UserID    string
	AddedBy   string
	CreatedAt string
}
type Database struct {
	conn *sql.DB
}

func NewDatabase(connStr string) (*Database, error) {
	conn, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	_, err = conn.Exec(`
        CREATE TABLE IF NOT EXISTS users (
        id VARCHAR(36) PRIMARY KEY,
        tg_id VARCHAR(100) UNIQUE NOT NULL,      -- Telegram ID для связи
        username VARCHAR(50) UNIQUE NOT NULL,    -- Уникальный ник (@durov)
        name VARCHAR(100) NOT NULL,               -- Отображаемое имя
        bio TEXT,                                 -- "О себе"
        age INTEGER,
        avatar_url TEXT,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP
    	)
    `)
	if err != nil {
		return nil, err
	}

	_, err = conn.Exec(`
        CREATE TABLE IF NOT EXISTS admins (
        user_id VARCHAR(100) PRIMARY KEY,         -- tg_id админа
        added_by VARCHAR(100),
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    	)
    `)
	if err != nil {
		return nil, err
	}

	_, err = conn.Exec(`
        CREATE TABLE IF NOT EXISTS posts (
        id VARCHAR(36) PRIMARY KEY,
        user_id VARCHAR(36) REFERENCES users(id) ON DELETE CASCADE,
        content TEXT NOT NULL,
        media_url VARCHAR(255),                    -- Ссылка на фото/видео
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP
    	)
    `)
	if err != nil {
		return nil, err
	}

	_, err = conn.Exec(`
        CREATE TABLE IF NOT EXISTS comments (
        id VARCHAR(36) PRIMARY KEY,
        post_id VARCHAR(36) REFERENCES posts(id) ON DELETE CASCADE,
        user_id VARCHAR(36) REFERENCES users(id) ON DELETE CASCADE,
        content TEXT NOT NULL,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    	)
    `)
	if err != nil {
		return nil, err
	}

	_, err = conn.Exec(`
    	CREATE TABLE IF NOT EXISTS subscriptions (
        follower_id VARCHAR(36) REFERENCES users(id) ON DELETE CASCADE,  -- кто подписан
        following_id VARCHAR(36) REFERENCES users(id) ON DELETE CASCADE, -- на кого подписан
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (follower_id, following_id)
    	)
	`)
	if err != nil {
		return nil, err
	}
	return &Database{conn: conn}, err
}

func (db *Database) IsAdmin(tgID string) (bool, error) {
	var exists bool
	err := db.conn.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM admins WHERE user_id = $1)", tgID,
	).Scan(&exists)
	return exists, err
}

func (db *Database) GetAllUsers(limit, offset int) ([]*UserDB, error) {
	rows, err := db.conn.Query(
		"SELECT id, name, username, age, created_at FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2", limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*UserDB
	for rows.Next() {
		user := &UserDB{}
		err := rows.Scan(&user.ID, &user.Name, &user.Username, &user.Age, &user.CreatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

func (db *Database) AddAdmin(telegramID, addedBy string) error {
	_, err := db.conn.Exec(
		"INSERT INTO admins (user_id, added_by) VALUES ($1, $2)",
		telegramID, addedBy,
	)
	return err
}

// RemoveAdmin - удаление администратора
func (db *Database) RemoveAdmin(telegramID string) error {
	result, err := db.conn.Exec("DELETE FROM admins WHERE user_id = $1", telegramID)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("admin not found")
	}
	return nil
}

func (db *Database) SaveUser(user *UserDB) error {
	_, err := db.conn.Exec(
		"INSERT INTO users (id, tg_id, username, name, bio, age, avatar_url, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)",
		user.ID, user.TgID, user.Username, user.Name, user.Bio, user.Age, user.AvatarURL, user.CreatedAt, user.UpdatedAt)
	return err
}

func (db *Database) GetUserByTgID(tgID string) (*UserDB, error) {
	user := &UserDB{}
	err := db.conn.QueryRow(
		`SELECT id, tg_id, username, name, bio, age, avatar_url, created_at, updated_at 
         FROM users WHERE tg_id = $1`, tgID,
	).Scan(&user.ID, &user.TgID, &user.Username, &user.Name, &user.Bio, &user.Age, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)
	return user, err
}

func (db *Database) GetUserByUsername(username string) (*UserDB, error) {
	user := &UserDB{}
	err := db.conn.QueryRow(
		`SELECT id, tg_id, username, name, bio, age, avatar_url, created_at, updated_at 
         FROM users WHERE username = $1`, username,
	).Scan(&user.ID, &user.TgID, &user.Username, &user.Name, &user.Bio, &user.Age, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)
	return user, err
}

func (db *Database) CreatePost(post *PostDB) error {
	_, err := db.conn.Exec(
		`INSERT INTO posts (id, user_id, content, media_url, created_at, updated_at) 
         VALUES ($1, $2, $3, $4, $5, $6)`,
		post.ID, post.AuthorID, post.Content, post.MediaURL, post.CreatedAt, post.UpdatedAt)
	return err
}

// database.go - добавить этот метод
func (db *Database) GetUserByID(id string) (*UserDB, error) {
	user := &UserDB{}
	err := db.conn.QueryRow(
		`SELECT id, tg_id, username, name, bio, age, avatar_url, created_at, updated_at 
         FROM users WHERE id = $1`, id,
	).Scan(
		&user.ID, &user.TgID, &user.Username, &user.Name,
		&user.Bio, &user.Age, &user.AvatarURL,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (db *Database) GetUserPosts(userID string, limit, offset int) ([]*PostDB, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, content, media_url, created_at, updated_at 
         FROM posts WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []*PostDB
	for rows.Next() {
		p := &PostDB{}
		err := rows.Scan(&p.ID, &p.AuthorID, &p.Content, &p.MediaURL, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, nil
}

func (db *Database) IsSubscribed(followerID, followingID string) (bool, error) {
	var exists bool
	err := db.conn.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM subscriptions WHERE follower_id = $1 AND following_id = $2)",
		followerID, followingID,
	).Scan(&exists)
	return exists, err
}

func (db *Database) AddComment(comment *CommentDB) error {
	_, err := db.conn.Exec(
		`INSERT INTO comments (id, post_id, user_id, content, created_at) 
         VALUES ($1, $2, $3, $4, $5)`,
		comment.ID, comment.PostID, comment.UserID, comment.Content, comment.CreatedAt)
	return err
}

func (db *Database) GetPostComments(postID string) ([]*CommentDB, error) {
	rows, err := db.conn.Query(
		`SELECT id, post_id, user_id, content, created_at 
         FROM comments WHERE post_id = $1 ORDER BY created_at`,
		postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []*CommentDB
	for rows.Next() {
		c := &CommentDB{}
		err := rows.Scan(&c.ID, &c.PostID, &c.UserID, &c.Content, &c.CreatedAt)
		if err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, nil
}

func (db *Database) Subscribe(followerID, followingID string) error {
	_, err := db.conn.Exec(
		`INSERT INTO subscriptions (follower_id, following_id) VALUES ($1, $2)`,
		followerID, followingID)
	return err
}

func (db *Database) Unsubscribe(followerID, followingID string) error {
	_, err := db.conn.Exec(
		`DELETE FROM subscriptions WHERE follower_id = $1 AND following_id = $2`,
		followerID, followingID)
	return err
}

func (db *Database) GetFeed(userID string, limit, offset int) ([]*PostDB, error) {
	rows, err := db.conn.Query(
		`SELECT p.id, p.user_id, p.content, p.media_url, p.created_at, p.updated_at 
         FROM posts p
         JOIN subscriptions s ON p.user_id = s.following_id
         WHERE s.follower_id = $1
         ORDER BY p.created_at DESC
         LIMIT $2 OFFSET $3`,
		userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []*PostDB
	for rows.Next() {
		p := &PostDB{}
		err := rows.Scan(&p.ID, &p.AuthorID, &p.Content, &p.MediaURL, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, nil
}

func (db *Database) GetUserPostsCount(userID string) (int32, error) {
	var count int32
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM posts WHERE user_id = $1", userID,
	).Scan(&count)
	return count, err
}

func (db *Database) GetFollowersCount(userID string) (int32, error) {
	var count int32
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM subscriptions WHERE following_id = $1", userID,
	).Scan(&count)
	return count, err
}

func (db *Database) GetFollowingCount(userID string) (int32, error) {
	var count int32
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM subscriptions WHERE follower_id = $1", userID,
	).Scan(&count)
	return count, err
}

func (db *Database) GetPostByID(postID string) (*PostDB, error) {
	post := &PostDB{}
	err := db.conn.QueryRow(
		`SELECT id, user_id, content, media_url, created_at, updated_at 
         FROM posts WHERE id = $1`, postID,
	).Scan(&post.ID, &post.AuthorID, &post.Content, &post.MediaURL, &post.CreatedAt, &post.UpdatedAt)

	if err != nil {
		return nil, err
	}
	return post, nil
}

func (db *Database) DeletePost(postID string) error {
	result, err := db.conn.Exec("DELETE FROM posts WHERE id = $1", postID)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("post not found")
	}
	return nil
}

func (db *Database) Close() error {
	return db.conn.Close()
}
