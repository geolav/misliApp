package main

import (
	pb "GOApp/proto/user/v1"
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"google.golang.org/grpc"
	//"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

//var hi = "что это и зачем:\n\nу меня нет цели обойти цукерберга и дурова - идеальные соц сети уже есть, а это всего лишь результат того, что мне стало интересно, как работают подобные вещи.\n\nвы (если пришли не просто так) тут для того, чтобы протестировать, как это работает. мне важно посмотреть, что будет происходить, когда появится какая-то нагрузка.\n\nне стоит спамить всяким \"sdhfsdhf\" и тому подобным, это я уже сделал. скорее правда попробуйте поделиться какими-то своими серьёзными или не совсем мыслями, ответить на чужие мысли, и так далее.\n\n\nчто тут можно делать:\n\nможно завести свой аккаунт, писать посты и делиться в них своими мыслями\n\nможно найти друга или подругу по нику в тг и почитать его / её мысли. можно и прокомментировать их - авторам интересно знать мнение других.\n\nможно прикреплять к постам ссылку на какой-то медиа-контент\n\n\nчто тут не стоит делать:\n\nне делитесь личными данными и тому подобным - конечно, над всеми данными стою лишь я, и, конечно, ничего делать с ними не собираюсь, но всё же не надо.\n\nне оскорбляйте других людей. опять же, вряд ли тут будет сильно много людей (конечно не будет), но всё-таки на всякий напишу.\n\n\nf.a.q.\n\nесли хотите видеть свои посты в ленте - подпишитесь на себя, это не запрещено.\n\n\"читы - бан, кемперство - бан, много мата - бан...\" - вас тут будет мало, если вообще кто-то будет, а может и вообще никто не прочитает это, так что управлять этим всем будет легко.\n\nесли хотите сделать комментарий к своему посту - найдите себя в поиске пользователей, найдите нужный пост, сделайте комментарий.\n\nесли хотите удалить один из своих постов - найдите себя в поиске пользователей, найдите нужный пост, удалите кнопкой \"удалить пост\", либо то же самое, но можно найти пост в ленте (если вы подписаны сами на себя).\n\nесли хотите удалить свой комментарий - извините, этой функции пока нет, TODO.\n\nесли хотите удалить свой профиль - извините, этой функции пока нет, TODO.\n\nесли хотите редактировать свой профиль - можете написать мне, я попробую добавить эту функцию специально ради вас...\n\nлайков тут пока нет - идея была в том, чтобы люди оценивали словами, а не тыком на сердечко. но мб автор передумает.\n\nс любыми предложениями или критикой (аккуратной) - тг @efnms, мне будет полезно понять, что было бы круто сделать."

var userStates = struct {
	sync.RWMutex
	m map[int64]string
}{m: make(map[int64]string)}

var userPages = struct {
	sync.RWMutex
	m map[int64]int
}{m: make(map[int64]int)}

var userPostsPages = struct {
	sync.RWMutex
	// ключ: chatID_targetTgID, значение: страница
	m map[string]int
}{m: make(map[string]int)}

var userData = struct {
	sync.RWMutex
	m map[int64]map[string]string
}{m: make(map[int64]map[string]string)}

var userTempPost = struct {
	sync.RWMutex
	m map[int64]map[string]string
}{m: make(map[int64]map[string]string)}

var userPostView = struct {
	sync.RWMutex
	// ключ: chatID_targetTgID, значение: структура с индексом текущего поста и списком ID постов
	m map[string]*UserPostViewState
}{m: make(map[string]*UserPostViewState)}

var feedView = struct {
	sync.RWMutex
	// ключ: chatID, значение: структура с индексом текущего поста и списком ID постов
	m map[int64]*FeedViewState
}{m: make(map[int64]*FeedViewState)}

type FeedViewState struct {
	PostIDs      []string
	CurrentIndex int
	TotalPosts   int
	Posts        []*pb.PostResponse // Сохраняем посты для доступа к информации об авторе
}

type UserPostViewState struct {
	PostIDs      []string
	CurrentIndex int
	TotalPosts   int
	UserTgID     string
	Username     string
}

func formatDate(dateStr string) string {
	t, err := time.Parse(time.RFC3339, dateStr)
	if err == nil {
		return t.Format("02.01.2006 15:04") // 21.02.2026 14:30
	}
	// Если ничего не получилось - верни как есть
	return dateStr
}

func getMainKeyboard() *models.ReplyKeyboardMarkup {
	return &models.ReplyKeyboardMarkup{
		Keyboard: [][]models.KeyboardButton{
			{
				{Text: "👤 Регистрация"},
				{Text: "🔍 Найти пользователя"},
			},
			{
				//{Text: "📋 Список пользователей"},
				{Text: "📊 Лента"},
				{Text: "👤 Мой профиль"},
			},
			{
				{Text: "📝 Создать пост"},
				{Text: "📰 Мои посты"},
			},
			//{
			//	{Text: "📊 Лента"},
			//	{Text: "➕ Подписаться"},
			//},
		},
		ResizeKeyboard: true,
	}
}

func main() {
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	userClient := pb.NewUserServiceClient(conn)

	//tgBot, err := bot.New("8533358993:AAG2rHQtgo0rIpRoyabh7Z5P97n355Yxp1Y")
	tgBot, err := bot.New("8606863856:AAFrvXCOlRhVxGGKGfQqFn3LenzZ1u6ls3I")
	if err != nil {
		log.Fatalf("failed to connect to bot: %v", err)
	}

	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact,
		func(ctx context.Context, b *bot.Bot, update *models.Update) {
			chatID := update.Message.Chat.ID
			//tgID := strconv.FormatInt(chatID, 10)
			//md := metadata.New(map[string]string{"tg_id": tgID})
			//ctxWithMeta := metadata.NewOutgoingContext(ctx, md)
			//_, err := userClient.GetUserByTgID(ctxWithMeta, &pb.GetUserByTgIDRequest{
			//	TgId: tgID,
			//})

			//text := "🚀 Добро пожаловать! Выберите действие:"
			text := "🚀 Добро пожаловать!\n\n" +
				//hi +
				"Для начала зарегистрируйтесь через кнопку '👤 Регистрация'\n\n" +
				"А после этого найдите профиль `@efnms` и прочитайте пост-посвящение"
			//if err != nil {
			//	text = "🚀 Добро пожаловать!\n\n" +
			//		//hi +
			//		"\n\n Для начала зарегистрируйтесь через кнопку '👤 Регистрация'\n\n" +
			//		"А после этого найдите профиль `@efnms` и прочитайте пост-посвящение"
			//}
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        text,
				ParseMode:   "Markdown",
				ReplyMarkup: getMainKeyboard(),
			})
		})

	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypeContains,
		func(ctx context.Context, b *bot.Bot, update *models.Update) {
			chatID := update.Message.Chat.ID
			data := update.Message.Text
			tgID := strconv.FormatInt(chatID, 10)

			userStates.RLock()
			state := userStates.m[chatID]
			userStates.RUnlock()

			if state != "" && (data == "👤 Регистрация" ||
				data == "🔍 Найти пользователя" ||
				data == "📋 Список пользователей" ||
				data == "👤 Мой профиль" ||
				data == "📝 Создать пост" ||
				data == "📰 Мои посты" ||
				data == "📊 Лента" ||
				data == "➕ Подписаться") {

				userStates.Lock()
				delete(userStates.m, chatID)
				userStates.Unlock()
				userData.Lock()
				delete(userData.m, chatID)
				userData.Unlock()
			} else if state != "" {
				handleUserInput(ctx, b, update, userClient)
				return
			}

			md := metadata.New(map[string]string{"tg_id": tgID})
			ctxWithMeta := metadata.NewOutgoingContext(ctx, md)
			switch data {
			case "👤 Регистрация":
				handleRegistration(ctxWithMeta, b, chatID, userClient)

			case "🔍 Найти пользователя":
				userStates.Lock()
				userStates.m[chatID] = "awaiting_find"
				userStates.Unlock()
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: chatID,
					Text:   "Введите username для поиска:",
				})

			case "📋 Список пользователей":
				showUserList(ctxWithMeta, b, chatID, userClient, 1)
			case "/users_list":
				showUserList(ctxWithMeta, b, chatID, userClient, 1)

			case "👤 Мой профиль":
				showMyProfile(ctxWithMeta, b, chatID, userClient)

			case "📝 Создать пост":
				userStates.Lock()
				userStates.m[chatID] = "awaiting_post_content"
				userStates.Unlock()
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: chatID,
					Text:   "📝 Введите текст вашего поста:",
				})

			case "📰 Мои посты":
				showMyPosts(ctxWithMeta, b, chatID, userClient, 1)

			case "📊 Лента":
				showFeed(ctxWithMeta, b, chatID, userClient, 1)

			case "➕ Подписаться":
				userStates.Lock()
				userStates.m[chatID] = "awaiting_subscribe"
				userStates.Unlock()
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: chatID,
					Text:   "Введите username пользователя, на которого хотите подписаться:",
				})

			default:
				// Проверяем, не является ли это командой для просмотра поста
				if strings.HasPrefix(data, "/post_") {
					postID := strings.TrimPrefix(data, "/post_")
					showPost(ctxWithMeta, b, chatID, userClient, postID)
				} else {
					b.SendMessage(ctx, &bot.SendMessageParams{
						ChatID:      chatID,
						Text:        "❓ Используйте кнопки меню",
						ReplyMarkup: getMainKeyboard(),
					})
				}
			}

		})
	// TODO pagination

	// Обработчик для inline кнопок пагинации
	tgBot.RegisterHandler(bot.HandlerTypeCallbackQueryData, "", bot.MatchTypePrefix,
		func(ctx context.Context, b *bot.Bot, update *models.Update) {
			callback := update.CallbackQuery
			if callback == nil {
				log.Println("❌ Callback is nil")
				return
			}

			msg := callback.Message.Message
			if msg == nil {
				log.Println("❌ Message is nil")
				return
			}

			// Получаем данные
			chatID := msg.Chat.ID
			messageID := msg.ID
			data := callback.Data
			fromUser := callback.From

			log.Printf("🔍 CALLBACK RECEIVED: data='%s', from=%d (%s), chat=%d",
				data, fromUser.ID, fromUser.Username, chatID)

			// Отвечаем на callback (убираем "часики" на кнопке)
			_, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
				CallbackQueryID: callback.ID,
			})

			if err != nil {
				log.Printf("❌ Failed to answer callback: %v", err)
			}

			tgID := strconv.FormatInt(chatID, 10)
			md := metadata.New(map[string]string{"tg_id": tgID})
			ctxMeta := metadata.NewOutgoingContext(ctx, md)

			switch {
			case strings.HasPrefix(data, "users_page_"):
				handleUsersPagination(ctxMeta, b, chatID, messageID, userClient, data)

			case strings.HasPrefix(data, "posts_page_"):
				handlePostsPagination(ctxMeta, b, chatID, messageID, userClient, data)

			case strings.HasPrefix(data, "feed_page_"):
				handleFeedPagination(ctxMeta, b, chatID, messageID, userClient, data)

			case strings.HasPrefix(data, "view_post_"):
				postID := strings.TrimPrefix(data, "view_post_")
				showPostByID(ctxMeta, b, chatID, userClient, postID)

			case data == "delete_post_start":
				// Запрашиваем ID поста для удаления
				userStates.Lock()
				userStates.m[chatID] = "awaiting_delete_post_id"
				userStates.Unlock()
				log.Printf("Удаление поста")

				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: chatID,
					Text:   "🗑️ Введите ID поста, который хотите удалить:",
				})

			case data == "create_post":
				userStates.Lock()
				userStates.m[chatID] = "awaiting_post_content"
				userStates.Unlock()
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: chatID,
					Text:   "📝 Введите текст вашего поста:",
				})

			case strings.HasPrefix(data, "comment_"):
				postID := strings.TrimPrefix(data, "comment_")
				// Сохраняем ID поста и переходим в режим ожидания комментария
				userTempPost.Lock()
				if userTempPost.m[chatID] == nil {
					userTempPost.m[chatID] = make(map[string]string)
				}
				userTempPost.m[chatID]["post_id"] = postID
				userTempPost.m[chatID]["message_id"] = fmt.Sprintf("%d", messageID)
				userTempPost.Unlock()

				userStates.Lock()
				userStates.m[chatID] = "awaiting_comment"
				userStates.Unlock()

				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: chatID,
					Text:   "💬 Введите текст комментария:",
				})

			case strings.HasPrefix(data, "delete_post_"):
				postID := strings.TrimPrefix(data, "delete_post_")
				log.Printf("🗑️ Handling delete_post: %s", postID)

				// Вместо прямого удаления, показываем подтверждение
				keyboard := &models.InlineKeyboardMarkup{
					InlineKeyboard: [][]models.InlineKeyboardButton{
						{
							{Text: "✅ Да, удалить", CallbackData: "confirm_delete_" + postID},
							{Text: "❌ Нет, отмена", CallbackData: "cancel_delete"},
						},
					},
				}

				// Редактируем сообщение с постом, добавляя подтверждение
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID:      chatID,
					Text:        fmt.Sprintf("🗑️ Вы уверены, что хотите удалить этот пост?\n\n🆔 `%s`", postID),
					ParseMode:   "Markdown",
					ReplyMarkup: keyboard,
				})

			//case strings.HasPrefix(data, "user_posts_next_"):
			//	log.Printf("CALL user pagination: %s", data)
			//	// Формат: user_posts_next_982648151_2
			//	// Убираем префикс "user_posts_next_", остается "982648151_2"
			//	remaining := strings.TrimPrefix(data, "user_posts_next_")
			//	parts := strings.Split(remaining, "_")
			//	if len(parts) == 2 {
			//		targetTgID := parts[0]
			//		page, _ := strconv.Atoi(parts[1])
			//		log.Printf("➡️ Next page for user %s: %d", targetTgID, page)
			//		handleUserPostsPagination(ctxMeta, b, chatID, messageID, userClient, targetTgID, int32(page), callback.ID)
			//	} else {
			//		log.Printf("❌ Invalid format for user_posts_next_: %s", data)
			//		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			//			CallbackQueryID: callback.ID,
			//			Text:            "❌ Ошибка формата данных",
			//			ShowAlert:       true,
			//		})
			//	}

			//case strings.HasPrefix(data, "user_posts_prev_"):
			//	// Формат: user_posts_prev_982648151_1
			//	remaining := strings.TrimPrefix(data, "user_posts_prev_")
			//	parts := strings.Split(remaining, "_")
			//	if len(parts) == 2 {
			//		targetTgID := parts[0]
			//		page, _ := strconv.Atoi(parts[1])
			//		log.Printf("⬅️ Previous page for user %s: %d", targetTgID, page)
			//		handleUserPostsPagination(ctxMeta, b, chatID, messageID, userClient, targetTgID, int32(page), callback.ID)
			//	} else {
			//		log.Printf("❌ Invalid format for user_posts_prev_: %s", data)
			//		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			//			CallbackQueryID: callback.ID,
			//			Text:            "❌ Ошибка формата данных",
			//			ShowAlert:       true,
			//		})
			//	}

			case strings.HasPrefix(data, "feed_nav_prev_"):
				// Формат: feed_nav_prev_2
				newIndexStr := strings.TrimPrefix(data, "feed_nav_prev_")
				newIndex, _ := strconv.Atoi(newIndexStr)

				log.Printf("📱 Feed navigation: prev, newIndex=%d", newIndex)

				// Обновляем состояние
				feedView.Lock()
				if state, exists := feedView.m[chatID]; exists {
					state.CurrentIndex = newIndex
				}
				feedView.Unlock()

				// Показываем пост с новым индексом
				showSingleFeedPost(ctxMeta, b, chatID, messageID, userClient, newIndex)

			case strings.HasPrefix(data, "feed_nav_next_"):
				// Формат: feed_nav_next_4
				newIndexStr := strings.TrimPrefix(data, "feed_nav_next_")
				newIndex, _ := strconv.Atoi(newIndexStr)

				log.Printf("📱 Feed navigation: next, newIndex=%d", newIndex)

				// Обновляем состояние
				feedView.Lock()
				if state, exists := feedView.m[chatID]; exists {
					state.CurrentIndex = newIndex
				}
				feedView.Unlock()

				// Показываем пост с новым индексом
				showSingleFeedPost(ctxMeta, b, chatID, messageID, userClient, newIndex)

			case strings.HasPrefix(data, "user_post_nav_"):
				// Формат: user_post_nav_123456789_next_2 или user_post_nav_123456789_prev_0
				parts := strings.Split(data, "_")
				// parts: ["user", "post", "nav", "123456789", "next", "2"]
				if len(parts) >= 6 {
					targetTgID := parts[3]
					direction := parts[4] // "next" или "prev"
					newIndex, _ := strconv.Atoi(parts[5])

					log.Printf("📱 Post navigation: user=%s, direction=%s, newIndex=%d",
						targetTgID, direction, newIndex)

					// Обновляем состояние
					key := fmt.Sprintf("%d_%s", chatID, targetTgID)
					userPostView.Lock()
					if state, exists := userPostView.m[key]; exists {
						state.CurrentIndex = newIndex
					}
					userPostView.Unlock()

					// Показываем пост с новым индексом
					showSingleUserPost(ctxMeta, b, chatID, messageID, userClient, targetTgID, newIndex)
				}

			case strings.HasPrefix(data, "user_posts_") &&
				!strings.HasPrefix(data, "user_posts_next_") &&
				!strings.HasPrefix(data, "user_posts_prev_") &&
				!strings.HasPrefix(data, "user_post_nav_"):
				// Формат: user_posts_123456789
				targetTgID := strings.TrimPrefix(data, "user_posts_")
				log.Printf("👤 Showing posts for user tg_id: %s", targetTgID)
				showUserPosts(ctxMeta, b, chatID, userClient, targetTgID, 1)

			case data == "back_to_search":
				userStates.Lock()
				userStates.m[chatID] = "awaiting_find"
				userStates.Unlock()
				b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: chatID,
					Text:   "Введите username для поиска:",
				})

			case strings.HasPrefix(data, "subscribe_"):
				followingID := strings.TrimPrefix(data, "subscribe_")
				log.Printf("➕ Subscribe clicked: followingID=%s", followingID)
				handleSubscribe(ctxMeta, b, chatID, userClient, followingID, messageID) // Добавлен messageID

			case data == "back_to_posts":
				// Возврат к списку постов
				log.Printf("📰 Handling back_to_posts")
				showMyPosts(ctxMeta, b, chatID, userClient, 1)

			case strings.HasPrefix(data, "unsubscribe_"):
				followingID := strings.TrimPrefix(data, "unsubscribe_")
				handleUnsubscribe(ctxMeta, b, chatID, userClient, followingID, messageID)

			case strings.HasPrefix(data, "confirm_delete_"):
				postID := strings.TrimPrefix(data, "confirm_delete_")
				simpleDeletePost(ctxMeta, b, chatID, userClient, postID, messageID)

			case data == "cancel_delete":
				b.EditMessageText(ctx, &bot.EditMessageTextParams{
					ChatID:    chatID,
					MessageID: messageID,
					Text:      "❌ Удаление отменено",
				})

			default:
				log.Printf("❓ Unknown callback data: %s", data)

			}
		})

	// В функции main, после других обработчиков, добавьте:
	tgBot.RegisterHandler(bot.HandlerTypeMessageText, "/del_", bot.MatchTypePrefix,
		func(ctx context.Context, b *bot.Bot, update *models.Update) {
			chatID := update.Message.Chat.ID
			text := update.Message.Text
			tgID := strconv.FormatInt(chatID, 10)

			// Создаем контекст с tg_id
			md := metadata.New(map[string]string{"tg_id": tgID})
			ctxWithMeta := metadata.NewOutgoingContext(ctx, md)

			// Извлекаем ID поста (удаляем "/del_")
			postID := strings.TrimPrefix(text, "/del_")

			if postID == "" {
				b.SendMessage(ctxWithMeta, &bot.SendMessageParams{
					ChatID: chatID,
					Text:   "❌ Укажите ID поста. Пример: /del_123e4567-e89b-12d3-a456-426614174000",
				})
				return
			}

			// Подтверждение удаления
			keyboard := &models.InlineKeyboardMarkup{
				InlineKeyboard: [][]models.InlineKeyboardButton{
					{
						{Text: "✅ Да, удалить", CallbackData: "confirm_delete_" + postID},
						{Text: "❌ Нет, отмена", CallbackData: "cancel_delete"},
					},
				},
			}

			b.SendMessage(ctxWithMeta, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("🗑️ Вы уверены, что хотите удалить пост %s?", postID),
				ReplyMarkup: keyboard,
			})
		})

	// Запускаем бота (это блокирующий вызов)
	tgBot.Start(context.Background())
}

func handleRegistration(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient) {
	tgID := strconv.FormatInt(chatID, 10)

	// Проверяем, не зарегистрирован ли уже
	resp, err := userClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{
		TgId: tgID,
	})

	if err == nil && resp != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("✅ Вы уже зарегистрированы как %s (@%s)", resp.Name, resp.Username),
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	// Начинаем процесс регистрации
	userStates.Lock()
	userStates.m[chatID] = "reg_name"
	userStates.Unlock()

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   "📝 Введите ваше имя:",
	})
}

func showMyProfile(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient) {
	tgID := strconv.FormatInt(chatID, 10)
	resp, err := userClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{TgId: tgID})
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Вы не зарегистрированы. Используйте кнопку '👤 Регистрация'",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}
	created := formatDate(resp.CreatedAt)
	msg := fmt.Sprintf("👤 **Мой профиль**\n\n"+
		"**Имя:** %s\n"+
		"**Username:** @%s\n"+
		//"**Возраст:** %d\n"+
		"**О себе:** %s\n"+
		//"**ID:** `%s`\n"+
		"**Подписчиков:** %d\n"+
		"**Подписок:** %d\n"+
		"**Постов:** %d\n"+
		"**Зарегистрирован:** %s\n",
		resp.Name, resp.Username,
		//resp.Age,
		resp.Bio,
		//resp.UserId,
		resp.FollowersCount,
		resp.FollowingCount,
		resp.PostsCount, created)

	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "📰 Мои посты", CallbackData: "back_to_posts"},
			},
			{
				{Text: "🗑️ Удалить пост", CallbackData: "delete_post_start"},
			},
			{
				{Text: "📝 Создать пост", CallbackData: "create_post"},
			},
		},
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        msg,
		ParseMode:   "Markdown",
		ReplyMarkup: keyboard,
	})
}

func createPost(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient, content, mediaURL string) {
	tgID := strconv.FormatInt(chatID, 10)
	resp, err := userClient.CreatePost(ctx, &pb.CreatePostRequest{
		TgId:     tgID,
		Content:  content,
		MediaUrl: mediaURL,
	})

	if err != nil {
		st, _ := status.FromError(err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("❌ Ошибка создания поста: %s", st.Message()),
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	if resp == nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Получен пустой ответ от сервера",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	// Логируем для отладки
	log.Printf("✅ Пост создан: ID=%s, Content=%s", resp.PostId, resp.Content)

	// Формируем сообщение о посте
	postText := fmt.Sprintf("✅ **Пост создан!**\n\n")
	postText += fmt.Sprintf("%s\n\n", resp.Content)

	if resp.MediaUrl != "" {
		postText += fmt.Sprintf("📎 [Ссылка на медиа](%s)\n", resp.MediaUrl)
	}

	postText += fmt.Sprintf("📅 %s\n", formatDate(resp.CreatedAt))

	// Создаем клавиатуру с действиями для поста - убеждаемся, что ID не пустой
	if resp.PostId == "" {
		log.Printf("❌ ОШИБКА: PostId пустой!")
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Ошибка: сервер вернул пост без ID",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "🗑️ Удалить", CallbackData: "delete_post_" + resp.PostId},
				{Text: "📰 Все мои посты", CallbackData: "back_to_posts"},
			},
		},
	}
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        postText,
		ParseMode:   "Markdown",
		ReplyMarkup: keyboard,
	})
}

func showMyPosts(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient, page int32) {
	tgID := strconv.FormatInt(chatID, 10)

	_, err := userClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{
		TgId: tgID,
	})

	userPages.Lock()
	userPages.m[chatID] = int(page)
	userPages.Unlock()

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Сначала зарегистрируйтесь через кнопку '👤 Регистрация'",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	resp, err := userClient.GetUserPosts(ctx, &pb.GetUserPostsRequest{
		TgId:     tgID,
		Page:     page,
		PageSize: 5,
	})

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Ошибка загрузки постов",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	if len(resp.Posts) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "📭 У вас пока нет постов",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	text := fmt.Sprintf("📰 **Мои посты (страница %d):**\n\n", page)

	for i, post := range resp.Posts {
		created := formatDate(post.CreatedAt)
		num := (int(page)-1)*5 + i + 1
		text += fmt.Sprintf("%d. **%s**\n   💬 Комментариев: %d\n   📅 %s\n   ID: `%s`\n\n",
			num, truncateText(post.Content, 50), post.CommentsCount, created, post.PostId)
	}

	// Клавиатура для пагинации
	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{}},
	}

	if page > 1 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{Text: "⬅️ Назад", CallbackData: fmt.Sprintf("posts_page_prev_%d", page-1)})
	}

	if len(resp.Posts) == 5 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{Text: "➡️ Вперед", CallbackData: fmt.Sprintf("posts_page_next_%d", page+1)})
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "Markdown",
		ReplyMarkup: keyboard,
	})
}

// ===== ПОКАЗ ПОСТА С КОММЕНТАРИЯМИ =====
func showPost(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient, postID string) {
	resp, err := userClient.GetPost(ctx, &pb.GetPostRequest{
		PostId: postID,
	})

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Пост не найден",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	created := formatDate(resp.Post.CreatedAt)

	text := fmt.Sprintf("📝 **Пост от @%s**\n\n"+
		"%s\n\n"+
		"📅 %s\n"+
		"💬 Комментариев: %d\n\n",
		resp.Post.AuthorUsername, resp.Post.Content, created, resp.Post.CommentsCount)

	if len(resp.Comments) > 0 {
		text += "**Комментарии:**\n"
		for _, comment := range resp.Comments {
			//if i >= 5 { // Показываем только первые 5 комментариев
			//	text += "...\n"
			//	break
			//}
			commentDate := formatDate(comment.CreatedAt)
			text += fmt.Sprintf("@%s: %s _(%s)_\n",
				comment.AuthorUsername, comment.Content, commentDate)
		}
	}

	// Клавиатура для действий
	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "💬 Комментировать", CallbackData: "comment_" + postID},
			},
			{
				{Text: "🔙 Назад", CallbackData: "back_to_posts"},
			},
		},
	}

	// Если это наш пост, добавляем кнопку удаления
	tgID := strconv.FormatInt(chatID, 10)
	if resp.Post.AuthorTgId == tgID {
		keyboard.InlineKeyboard = append([][]models.InlineKeyboardButton{
			{{Text: "🗑️ Удалить пост", CallbackData: "delete_post_" + postID}},
		}, keyboard.InlineKeyboard...)
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "Markdown",
		ReplyMarkup: keyboard,
	})
}

// Простое удаление поста по ID
func simpleDeletePost(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient, postID string, messageID int) {
	log.Printf("🗑️ simpleDeletePost: postID=%s", postID)

	tgID := strconv.FormatInt(chatID, 10)

	// Получаем ID пользователя
	userResp, err := userClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{
		TgId: tgID,
	})

	if err != nil {
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "❌ Ошибка: пользователь не найден",
		})
		return
	}

	// Вызываем DeletePost
	resp, err := userClient.DeletePost(ctx, &pb.DeletePostRequest{
		PostId: postID,
		UserId: userResp.UserId,
	})

	if err != nil {
		st, _ := status.FromError(err)
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      fmt.Sprintf("❌ Ошибка: %s", st.Message()),
		})
		return
	}

	if resp.Success {
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      fmt.Sprintf("✅ Пост успешно удален!"),
		})

		// Показываем обновленный список постов
		//time.Sleep(1 * time.Second)
		//showMyPosts(ctx, b, chatID, userClient, 1)
	} else {
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      fmt.Sprintf("❌ %s", resp.Message),
		})
	}
}

func showFeed(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient, page int32) {
	tgID := strconv.FormatInt(chatID, 10)

	// Получаем ID пользователя
	userResp, err := userClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{
		TgId: tgID,
	})

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Сначала зарегистрируйтесь",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	resp, err := userClient.GetFeed(ctx, &pb.GetFeedRequest{
		UserId:   userResp.UserId,
		Page:     page,
		PageSize: 100,
	})

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Ошибка загрузки ленты",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	if len(resp.Posts) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "📭 Лента пуста. Подпишитесь на пользователей!",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	//text := fmt.Sprintf("📊 **Лента (страница %d):**\n\n", page)

	postIDs := make([]string, len(resp.Posts))
	for i, post := range resp.Posts {
		postIDs[i] = post.PostId
	}

	feedView.Lock()
	feedView.m[chatID] = &FeedViewState{
		PostIDs:      postIDs,
		CurrentIndex: 0,
		TotalPosts:   len(resp.Posts),
		Posts:        resp.Posts,
	}
	feedView.Unlock()

	msg, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      "Загрузка...",
		ParseMode: "Markdown",
	})

	if err == nil && msg != nil {
		// Показываем первый пост ленты с messageID
		showSingleFeedPost(ctx, b, chatID, msg.ID, userClient, 0)
	} else {
		// Если не удалось получить ID, показываем без него
		showSingleFeedPost(ctx, b, chatID, 0, userClient, 0)
	}

	//for i, post := range resp.Posts {
	//	created := formatDate(post.CreatedAt)
	//	text += fmt.Sprintf("%d. **@%s**: %s\n   📅 %s\n   ID: `%s`\n\n",
	//		i+1, post.AuthorUsername, truncateText(post.Content, 100), created, post.PostId)
	//}
	//
	//// Клавиатура для пагинации
	//keyboard := &models.InlineKeyboardMarkup{
	//	InlineKeyboard: [][]models.InlineKeyboardButton{{}},
	//}
	//
	//if page > 1 {
	//	keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
	//		models.InlineKeyboardButton{Text: "⬅️ Назад", CallbackData: fmt.Sprintf("feed_page_prev_%d", page-1)})
	//}
	//
	//if len(resp.Posts) == 5 {
	//	keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
	//		models.InlineKeyboardButton{Text: "➡️ Вперед", CallbackData: fmt.Sprintf("feed_page_next_%d", page+1)})
	//}
	//
	//b.SendMessage(ctx, &bot.SendMessageParams{
	//	ChatID:      chatID,
	//	Text:        text,
	//	ParseMode:   "Markdown",
	//	ReplyMarkup: keyboard,
	//})
}

func showSingleFeedPost(ctx context.Context, b *bot.Bot, chatID int64, messageID int, userClient pb.UserServiceClient, index int) {
	feedView.RLock()
	state, exists := feedView.m[chatID]
	feedView.RUnlock()

	if !exists || index < 0 || index >= len(state.PostIDs) {
		log.Printf("❌ Invalid feed index or state not found")
		return
	}

	// Обновляем индекс
	state.CurrentIndex = index

	// Получаем пост по ID
	postID := state.PostIDs[index]
	resp, err := userClient.GetPost(ctx, &pb.GetPostRequest{
		PostId: postID,
	})
	if err != nil {
		log.Printf("❌ Failed to get post: %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Ошибка загрузки поста",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	// Используем данные из сохраненного состояния для информации об авторе
	post := state.Posts[index]

	// Формируем текст поста
	created := formatDate(resp.Post.CreatedAt)
	text := fmt.Sprintf("📊 **Лента** (%d из %d)\n\n", index+1, state.TotalPosts)
	text += fmt.Sprintf("📝 **Пост от @%s**\n\n", post.AuthorUsername)
	text += fmt.Sprintf("%s\n\n", resp.Post.Content)

	if resp.Post.MediaUrl != "" {
		text += fmt.Sprintf("📎 [Медиа](%s)\n", resp.Post.MediaUrl)
	}

	text += fmt.Sprintf("📅 %s\n", created)
	text += fmt.Sprintf("💬 Комментариев: %d\n\n", resp.Post.CommentsCount)

	if len(resp.Comments) > 0 {
		text += "**Комментарии:**\n"
		for _, comment := range resp.Comments {
			commentDate := formatDate(comment.CreatedAt)
			text += fmt.Sprintf("@%s: %s _(%s)_\n",
				comment.AuthorUsername, comment.Content, commentDate)
		}
	}

	// Создаем клавиатуру для навигации
	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{}, // Первая строка - навигация
			{}, // Вторая строка - действия
		},
	}

	// Кнопки навигации (первая строка)
	if index > 0 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{
				Text:         "⬅️ Предыдущий",
				CallbackData: fmt.Sprintf("feed_nav_prev_%d", index-1),
			})
	}

	if index < state.TotalPosts-1 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{
				Text:         "Следующий ➡️",
				CallbackData: fmt.Sprintf("feed_nav_next_%d", index+1),
			})
	}

	// Кнопки действий (вторая строка)
	keyboard.InlineKeyboard[1] = append(keyboard.InlineKeyboard[1],
		models.InlineKeyboardButton{
			Text:         "💬 Комментировать",
			CallbackData: "comment_" + postID,
		})

	// Если это наш пост, добавляем кнопку удаления
	tgID := strconv.FormatInt(chatID, 10)
	if resp.Post.AuthorTgId == tgID {
		// Вставляем кнопку удаления в начало второй строки
		deleteBtn := []models.InlineKeyboardButton{
			{Text: "🗑️ Удалить", CallbackData: "delete_post_" + postID},
		}
		keyboard.InlineKeyboard[1] = append(deleteBtn, keyboard.InlineKeyboard[1]...)
	}

	if messageID > 0 {
		// Редактируем существующее сообщение
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "Markdown",
			ReplyMarkup: keyboard,
		})
	} else {
		// Отправляем новое (первый раз)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        text,
			ParseMode:   "Markdown",
			ReplyMarkup: keyboard,
		})
	}
}

func handleUnsubscribe(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient, followingID string, messageID int) {
	tgID := strconv.FormatInt(chatID, 10)
	log.Printf("➖ handleUnsubscribe called: followingID=%s, messageID=%d", followingID, messageID)

	// Получаем текущего пользователя
	currentUser, err := userClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{
		TgId: tgID,
	})

	if err != nil {
		log.Printf("❌ GetUserByTgID error: %v", err)

		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: fmt.Sprintf("%d", messageID),
			Text:            "❌ Сначала зарегистрируйтесь",
			ShowAlert:       true,
		})
		return
	}
	log.Printf("📤 Calling Unsubscribe with follower=%s, following=%s", currentUser.UserId, followingID)

	// Вызываем Unsubscribe
	resp, err := userClient.Unsubscribe(ctx, &pb.UnsubscribeRequest{
		FollowerId:  currentUser.UserId,
		FollowingId: followingID,
	})

	if err != nil {
		st, _ := status.FromError(err)
		log.Printf("❌ Unsubscribe RPC error: %v, code=%v, message=%s", err, st.Code(), st.Message())
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: fmt.Sprintf("%d", messageID),
			Text:            "❌ " + st.Message(),
			ShowAlert:       true,
		})
		return
	}
	log.Printf("✅ Unsubscribe response: success=%v, message=%s", resp.Success, resp.Message)

	if resp.Success {
		userTempPost.RLock()
		targetTgID := userTempPost.m[chatID]["target_tg_id"]
		targetName := userTempPost.m[chatID]["target_name"]
		targetUsername := userTempPost.m[chatID]["target_username"]
		targetBio := userTempPost.m[chatID]["target_bio"]
		targetFollowing := userTempPost.m[chatID]["target_following"]
		targetPosts := userTempPost.m[chatID]["target_posts"]
		targetCreated := userTempPost.m[chatID]["target_created"]
		followersStr := userTempPost.m[chatID]["target_followers"]
		userTempPost.RUnlock()

		followers, _ := strconv.Atoi(followersStr)
		followers--
		if followers < 0 {
			followers = 0
		}
		newFollowersStr := fmt.Sprintf("%d", followers)

		userTempPost.Lock()
		userTempPost.m[chatID]["target_followers"] = newFollowersStr
		userTempPost.m[chatID]["is_subscribed"] = "false"
		userTempPost.Unlock()

		keyboard := &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "➕ Подписаться", CallbackData: "subscribe_" + followingID},
					{Text: "📰 Посты", CallbackData: "user_posts_" + targetTgID},
				},
			},
		}
		created := formatDate(targetCreated)
		msg := fmt.Sprintf("👤 **%s** (@%s)\n"+
			"О себе: %s\n"+
			"Подписчиков: %d\n"+
			"Подписок: %s\n"+
			"Постов: %s\n"+
			"Зарегистрирован: %s\n",
			targetName, targetUsername,
			targetBio, followers, targetFollowing, targetPosts, created)

		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        msg,
			ParseMode:   "Markdown",
			ReplyMarkup: keyboard,
		})

		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: fmt.Sprintf("%d", messageID),
			Text:            "✅ Вы отписались",
			ShowAlert:       false,
		})

	} else {
		log.Printf("❌ Unsubscribe failed: %s", resp.Message)

		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: fmt.Sprintf("%d", messageID),
			Text:            resp.Message,
			ShowAlert:       true,
		})
	}
}

func handleSubscribe(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient, followingID string, messageID int) {
	tgID := strconv.FormatInt(chatID, 10)
	log.Printf("➕ handleSubscribe: followingID=%s", followingID)

	currUser, err := userClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{
		TgId: tgID,
	})

	if err != nil {
		log.Printf("❌ GetUserByTgID error: %v", err)

		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: fmt.Sprintf("%d", messageID),
			Text:            "❌ Сначала зарегистрируйтесь",
			ShowAlert:       true,
		})
		return
	}
	log.Printf("📤 Calling Subscribe with follower=%s, following=%s", currUser.UserId, followingID)

	subResp, err := userClient.Subscribe(ctx, &pb.SubscribeRequest{
		FollowerId:  currUser.UserId,
		FollowingId: followingID,
	})

	if err != nil {
		st, _ := status.FromError(err)
		log.Printf("❌ Subscribe RPC error: %v, code=%v, message=%s", err, st.Code(), st.Message())
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: fmt.Sprintf("%d", messageID),
			Text:            "❌ " + st.Message(),
			ShowAlert:       true,
		})
		return
	}

	log.Printf("✅ Subscribe response: success=%v, message=%s", subResp.Success, subResp.Message)

	if subResp.Success {
		userTempPost.RLock()
		targetTgID := userTempPost.m[chatID]["target_tg_id"]
		targetName := userTempPost.m[chatID]["target_name"]
		targetUsername := userTempPost.m[chatID]["target_username"]
		targetBio := userTempPost.m[chatID]["target_bio"]
		targetFollowing := userTempPost.m[chatID]["target_following"]
		targetPosts := userTempPost.m[chatID]["target_posts"]
		targetCreated := userTempPost.m[chatID]["target_created"]
		followersStr := userTempPost.m[chatID]["target_followers"]
		userTempPost.RUnlock()

		followers, _ := strconv.Atoi(followersStr)
		followers++
		newFollowersStr := fmt.Sprintf("%d", followers)

		userTempPost.Lock()
		userTempPost.m[chatID]["target_followers"] = newFollowersStr
		userTempPost.m[chatID]["is_subscribed"] = "true"
		userTempPost.Unlock()

		log.Printf("📌 targetTgID from storage: %s", targetTgID)
		// Обновляем кнопку в сообщении
		keyboard := &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "➖ Отписаться", CallbackData: "unsubscribe_" + followingID},
					{Text: "📰 Посты", CallbackData: "user_posts_" + targetTgID},
				},
			},
		}

		created := formatDate(targetCreated)
		msg := fmt.Sprintf("👤 **%s** (@%s)\n"+
			"О себе: %s\n"+
			"Подписчиков: %d\n"+
			"Подписок: %s\n"+
			"Постов: %s\n"+
			"Зарегистрирован: %s\n",
			targetName, targetUsername,
			targetBio, followers, targetFollowing, targetPosts, created)

		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        msg,
			ParseMode:   "Markdown",
			ReplyMarkup: keyboard,
		})

		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: fmt.Sprintf("%d", messageID),
			Text:            "✅ Подписка оформлена",
			ShowAlert:       false,
		})
	} else {
		log.Printf("❌ Subscribe failed: %s", subResp.Message)
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: fmt.Sprintf("%d", messageID),
			Text:            subResp.Message,
			ShowAlert:       true,
		})
	}
}

func handleUserInput(ctx context.Context, b *bot.Bot, update *models.Update, userClient pb.UserServiceClient) {
	chatID := update.Message.Chat.ID
	data := update.Message.Text
	tgID := strconv.FormatInt(chatID, 10)

	userStates.RLock()
	state := userStates.m[chatID]
	userStates.RUnlock()
	md := metadata.New(map[string]string{"tg_id": tgID})
	ctxMeta := metadata.NewOutgoingContext(ctx, md)

	switch state {
	case "reg_name":
		userData.Lock()
		if userData.m[chatID] == nil {
			userData.m[chatID] = make(map[string]string)
		}
		userData.m[chatID]["name"] = data
		userData.Unlock()

		telegramUsername := update.Message.From.Username
		userData.Lock()
		userData.m[chatID]["username"] = telegramUsername
		userData.Unlock()

		userStates.Lock()
		userStates.m[chatID] = "reg_bio"
		userStates.Unlock()

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      fmt.Sprintf("✅ Ваш username: @%s\n\n📝 Расскажите о себе (биография) или отправьте `-` чтобы пропустить:", telegramUsername),
			ParseMode: "Markdown",
		})

		//b.SendMessage(ctx, &bot.SendMessageParams{
		//	ChatID: chatID,
		//	Text:   "📝 Введите username (без @):",
		//})

	case "reg_username":
		//username := strings.TrimPrefix(data, "@")  //  TODO проверить
		username := update.Message.Chat.Username
		log.Printf("log in user wuth tg_username: %s", username)

		userData.Lock()
		userData.m[chatID]["username"] = username
		userData.Unlock()

		userStates.Lock()
		//userStates.m[chatID] = "reg_age"
		userStates.m[chatID] = "reg_bio"
		userStates.Unlock()

		//b.SendMessage(ctx, &bot.SendMessageParams{
		//	ChatID: chatID,
		//	Text:   "📝 Введите ваш возраст",
		//})

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      "📝 Расскажите о себе (биография) или отправьте `-` чтобы пропустить:",
			ParseMode: "Markdown",
		})

	case "reg_age":
		age, err := strconv.Atoi(data)
		if err != nil {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: chatID,
				Text:   "❌ Возраст должен быть числом. Попробуйте ещё раз:",
			})
			return
		}

		userData.Lock()
		userData.m[chatID]["age"] = strconv.Itoa(age)
		userData.Unlock()

		userStates.Lock()
		userStates.m[chatID] = "reg_bio"
		userStates.Unlock()

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      "📝 Расскажите о себе (биография) или отправьте `-` чтобы пропустить:",
			ParseMode: "Markdown",
		})

	case "reg_bio":
		bio := data
		if bio == "-" {
			bio = ""
		}
		userData.RLock()
		name := userData.m[chatID]["name"]
		username := userData.m[chatID]["username"]
		ageStr := userData.m[chatID]["age"]
		userData.RUnlock()

		age, _ := strconv.Atoi(ageStr)

		_, err := userClient.RegisterUser(ctxMeta, &pb.RegisterUserRequest{
			TgId:      tgID,
			Username:  username,
			Name:      name,
			Age:       int32(age),
			Bio:       bio,
			AvatarUrl: "", // Можно добавить позже
		})

		userStates.Lock()
		delete(userStates.m, chatID)
		userStates.Unlock()

		userData.Lock()
		delete(userData.m, chatID)
		userData.Unlock()

		if err != nil {
			st, _ := status.FromError(err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("❌ Ошибка регистрации: %s", st.Message()),
				ReplyMarkup: getMainKeyboard(),
			})
			return
		}

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("✅ Регистрация успешна! Добро пожаловать, %s!", name),
			ReplyMarkup: getMainKeyboard(),
		})

	case "awaiting_find":
		username := strings.TrimPrefix(data, "@")
		targetUser, err := userClient.GetUserByUsername(ctx, &pb.GetUserRequest{
			Username: username,
		})
		userStates.Lock()
		delete(userStates.m, chatID)
		userStates.Unlock()

		if err != nil {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        "❌ Пользователь не найден",
				ReplyMarkup: getMainKeyboard(),
			})
			return
		}

		userTempPost.Lock()
		if userTempPost.m[chatID] == nil {
			userTempPost.m[chatID] = make(map[string]string)
		}
		userTempPost.m[chatID]["target_user_id"] = targetUser.UserId
		userTempPost.m[chatID]["target_tg_id"] = targetUser.TgId
		userTempPost.m[chatID]["target_username"] = targetUser.Username
		userTempPost.m[chatID]["target_name"] = targetUser.Name
		userTempPost.m[chatID]["target_bio"] = targetUser.Bio
		userTempPost.m[chatID]["target_followers"] = fmt.Sprintf("%d", targetUser.FollowersCount)
		userTempPost.m[chatID]["target_following"] = fmt.Sprintf("%d", targetUser.FollowingCount)
		userTempPost.m[chatID]["target_posts"] = fmt.Sprintf("%d", targetUser.PostsCount)
		userTempPost.m[chatID]["target_created"] = targetUser.CreatedAt
		userTempPost.Unlock()

		currentUser, err := userClient.GetUserByTgID(ctxMeta, &pb.GetUserByTgIDRequest{
			TgId: tgID,
		})

		if err != nil {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        "❌ Сначала зарегистрируйтесь",
				ReplyMarkup: getMainKeyboard(),
			})
			return
		}

		subResp, err := userClient.IsSubscribed(ctx, &pb.IsSubscribedRequest{
			FollowerId:  currentUser.UserId,
			FollowingId: targetUser.UserId,
		})

		subscribeButtonText := "➕ Подписаться"
		subscribeButtonData := "subscribe_" + targetUser.UserId

		if err == nil && subResp.IsSubscribed {
			subscribeButtonText = "➖ Отписаться"
			subscribeButtonData = "unsubscribe_" + targetUser.UserId
		}

		created := formatDate(targetUser.CreatedAt)
		msg := fmt.Sprintf("👤 **%s** (@%s)\n"+
			//"ID: `%s`\n"+
			//"Возраст: %d\n"+
			"О себе: %s\n"+
			"Подписчиков: %d\n"+
			"Подписок: %d\n"+
			"Постов: %d\n"+
			"Зарегистрирован: %s\n",
			targetUser.Name, targetUser.Username,
			//resp.UserId,
			//resp.Age,
			targetUser.Bio, targetUser.FollowersCount,
			targetUser.FollowingCount,
			targetUser.PostsCount, created)

		keyboard := &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: subscribeButtonText, CallbackData: subscribeButtonData},
					{Text: "📰 Посты", CallbackData: "user_posts_" + targetUser.TgId},
				},
			},
		}

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        msg,
			ParseMode:   "Markdown",
			ReplyMarkup: keyboard,
		})

	case "awaiting_delete_post_id":
		postID := strings.TrimSpace(data)

		if postID == "" {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        "❌ ID поста не может быть пустым",
				ReplyMarkup: getMainKeyboard(),
			})
			userStates.Lock()
			delete(userStates.m, chatID)
			userStates.Unlock()
			return
		}

		// Подтверждение удаления
		keyboard := &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "✅ Да, удалить", CallbackData: "confirm_delete_" + postID},
					{Text: "❌ Нет, отмена", CallbackData: "cancel_delete"},
				},
			},
		}

		// Очищаем состояние, но сохраним ID поста для подтверждения
		userStates.Lock()
		delete(userStates.m, chatID)
		userStates.Unlock()

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("🗑️ Вы уверены, что хотите удалить пост `%s`?", postID),
			ParseMode:   "Markdown",
			ReplyMarkup: keyboard,
		})

	case "awaiting_post_content":
		userTempPost.Lock()
		if userTempPost.m[chatID] == nil {
			userTempPost.m[chatID] = make(map[string]string)
		}
		userTempPost.m[chatID]["content"] = data
		userTempPost.Unlock()

		userStates.Lock()
		userStates.m[chatID] = "awaiting_post_media"
		userStates.Unlock()

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "📎 Отправьте ссылку на медиа (фото/видео) или отправьте '-' чтобы пропустить:",
		})

	case "awaiting_comment":
		postID := userTempPost.m[chatID]["post_id"]
		if postID == "" {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        "❌ Ошибка: ID поста не найден",
				ReplyMarkup: getMainKeyboard(),
			})
			return
		}

		messageIDStr := userTempPost.m[chatID]["message_id"]
		currentMessageID, _ := strconv.Atoi(messageIDStr)

		_, err := userClient.AddComment(ctx, &pb.AddCommentRequest{
			PostId:  postID,
			TgId:    tgID,
			Content: data,
		})

		// Очищаем состояния
		userStates.Lock()
		delete(userStates.m, chatID)
		userStates.Unlock()

		userTempPost.Lock()
		delete(userTempPost.m, chatID)
		userTempPost.Unlock()

		if err != nil {
			st, _ := status.FromError(err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("❌ Ошибка добавления комментария: %s", st.Message()),
				ReplyMarkup: getMainKeyboard(),
			})
			return
		}

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "✅ Комментарий добавлен!",
			ReplyMarkup: getMainKeyboard(),
		})

		if currentMessageID > 0 {
			// Определяем, откуда пришли (лента или посты пользователя)
			feedView.RLock()
			_, isFeed := feedView.m[chatID]
			feedView.RUnlock()

			if isFeed {
				// Если мы в ленте, обновляем пост в ленте
				feedView.RLock()
				state := feedView.m[chatID]
				currentIndex := state.CurrentIndex
				feedView.RUnlock()
				showSingleFeedPost(ctx, b, chatID, currentMessageID, userClient, currentIndex)
			} else {
				// Если мы в постах пользователя, обновляем пост пользователя
				userPostView.RLock()
				var targetTgID string
				var currentIndex int
				for key, state := range userPostView.m {
					if strings.HasPrefix(key, fmt.Sprintf("%d_", chatID)) {
						targetTgID = strings.TrimPrefix(key, fmt.Sprintf("%d_", chatID))
						currentIndex = state.CurrentIndex
						break
					}
				}
				userPostView.RUnlock()

				if targetTgID != "" {
					showSingleUserPost(ctx, b, chatID, currentMessageID, userClient, targetTgID, currentIndex)
				}
			}
		} else {
			// Если не знаем messageID, показываем пост через showPostByID (старое поведение)
			showPostByID(ctx, b, chatID, userClient, postID)
		}

	case "awaiting_post_media":
		mediaURL := data
		if mediaURL == "-" {
			mediaURL = ""
		}
		userTempPost.RLock()
		content := userTempPost.m[chatID]["content"]
		userTempPost.RUnlock()

		createPost(ctxMeta, b, chatID, userClient, content, mediaURL)

		userStates.Lock()
		delete(userStates.m, chatID)
		userStates.Unlock()

		userTempPost.Lock()
		delete(userTempPost.m, chatID)
		userTempPost.Unlock()

	case "awaiting_subscribe":
		username := strings.TrimPrefix(data, "@")
		targetUser, err := userClient.GetUserByUsername(ctx, &pb.GetUserRequest{
			Username: username,
		})
		if err != nil {
			userStates.Lock()
			delete(userStates.m, chatID)
			userStates.Unlock()

			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        "❌ Пользователь не найден",
				ReplyMarkup: getMainKeyboard(),
			})
			return
		}

		currUser, err := userClient.GetUserByTgID(ctxMeta, &pb.GetUserByTgIDRequest{
			TgId: tgID,
		})

		if err != nil {
			userStates.Lock()
			delete(userStates.m, chatID)
			userStates.Unlock()

			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        "❌ Сначала зарегистрируйтесь",
				ReplyMarkup: getMainKeyboard(),
			})
			return
		}

		subResp, err := userClient.Subscribe(ctxMeta, &pb.SubscribeRequest{
			FollowerId:  currUser.UserId,
			FollowingId: targetUser.UserId,
		})

		userStates.Lock()
		delete(userStates.m, chatID)
		userStates.Unlock()

		if err != nil {
			st, _ := status.FromError(err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("❌ Ошибка: %s", st.Message()),
				ReplyMarkup: getMainKeyboard(),
			})
			return
		}

		if subResp.Success {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("✅ Вы подписались на @%s", username),
				ReplyMarkup: getMainKeyboard(),
			})
		} else {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID:      chatID,
				Text:        fmt.Sprintf("ℹ️ %s", subResp.Message),
				ReplyMarkup: getMainKeyboard(),
			})
		}

	}
}

// Функция для показа постов пользователя по его tg_id (по аналогии с showMyPosts)
//func showUserPosts(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient, targetTgID string, page int32) {
//	log.Printf("📰 showUserPosts: targetTgID=%s, page=%d", targetTgID, page)
//	pageKey := fmt.Sprintf("%d_%s", chatID, targetTgID)
//	userPostsPages.Lock()
//	userPostsPages.m[pageKey] = int(page)
//	userPostsPages.Unlock()
//	// Получаем информацию о пользователе для отображения
//	userInfo, err := userClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{
//		TgId: targetTgID,
//	})
//
//	if err != nil {
//		log.Printf("❌ Failed to get user info: %v", err)
//		b.SendMessage(ctx, &bot.SendMessageParams{
//			ChatID:      chatID,
//			Text:        "❌ Пользователь не найден",
//			ReplyMarkup: getMainKeyboard(),
//		})
//		return
//	}
//
//	// Получаем посты пользователя
//	resp, err := userClient.GetUserPosts(ctx, &pb.GetUserPostsRequest{
//		TgId:     targetTgID,
//		Page:     page,
//		PageSize: 5,
//	})
//
//	if err != nil {
//		log.Printf("❌ GetUserPosts error: %v", err)
//		b.SendMessage(ctx, &bot.SendMessageParams{
//			ChatID:      chatID,
//			Text:        "❌ Ошибка загрузки постов",
//			ReplyMarkup: getMainKeyboard(),
//		})
//		return
//	}
//
//	if len(resp.Posts) == 0 {
//		b.SendMessage(ctx, &bot.SendMessageParams{
//			ChatID:      chatID,
//			Text:        fmt.Sprintf("📭 У пользователя @%s пока нет постов", userInfo.Username),
//			ReplyMarkup: getMainKeyboard(),
//		})
//		return
//	}
//
//	text := fmt.Sprintf("📰 **Посты пользователя @%s (страница %d):**\n\n", userInfo.Username, page)
//
//	for i, post := range resp.Posts {
//		created := formatDate(post.CreatedAt)
//		num := (int(page)-1)*5 + i + 1
//		cleanContent := sanitizeUTF8(post.Content)
//		short := truncateText(cleanContent, 50)
//		text += fmt.Sprintf("%d. **%s**\n   💬 Комментариев: %d\n   📅 %s\n   ID: `%s`\n\n",
//			num, short, post.CommentsCount, created, post.PostId)
//	}
//
//	// Клавиатура для пагинации (по аналогии с showMyPosts)
//	keyboard := &models.InlineKeyboardMarkup{
//		InlineKeyboard: [][]models.InlineKeyboardButton{{}},
//	}
//
//	if page > 1 {
//		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
//			models.InlineKeyboardButton{Text: "⬅️ Назад", CallbackData: fmt.Sprintf("user_posts_prev_%s_%d", targetTgID, page-1)})
//	}
//
//	if len(resp.Posts) == 5 {
//		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
//			models.InlineKeyboardButton{Text: "➡️ Вперед", CallbackData: fmt.Sprintf("user_posts_next_%s_%d", targetTgID, page+1)})
//	}
//
//	// Добавляем кнопку "Назад к поиску"
//	keyboard.InlineKeyboard = append(
//		keyboard.InlineKeyboard,
//		[]models.InlineKeyboardButton{
//			{Text: "🔙 Назад к поиску", CallbackData: "back_to_search"},
//		},
//	)
//	text = sanitizeUTF8(text)
//	b.SendMessage(ctx, &bot.SendMessageParams{
//		ChatID:      chatID,
//		Text:        text,
//		ParseMode:   "Markdown",
//		ReplyMarkup: keyboard,
//	})
//}

func showUserPosts(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient, targetTgID string, page int32) {
	log.Printf("📰 showUserPosts: targetTgID=%s, page=%d", targetTgID, page)

	// Получаем информацию о пользователе
	userInfo, err := userClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{
		TgId: targetTgID,
	})
	if err != nil {
		log.Printf("❌ Failed to get user info: %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Пользователь не найден",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	// Получаем ВСЕ посты пользователя (без пагинации, но можно ограничить)
	// Здесь используем большой pageSize, чтобы получить все посты
	resp, err := userClient.GetUserPosts(ctx, &pb.GetUserPostsRequest{
		TgId:     targetTgID,
		Page:     1,
		PageSize: 100, // Достаточно большое число
	})
	if err != nil {
		log.Printf("❌ GetUserPosts error: %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Ошибка загрузки постов",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	if len(resp.Posts) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        fmt.Sprintf("📭 У пользователя @%s пока нет постов", userInfo.Username),
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	// Сохраняем состояние для этого пользователя
	key := fmt.Sprintf("%d_%s", chatID, targetTgID)

	// Собираем все ID постов
	postIDs := make([]string, len(resp.Posts))
	for i, post := range resp.Posts {
		postIDs[i] = post.PostId
	}

	userPostView.Lock()
	userPostView.m[key] = &UserPostViewState{
		PostIDs:      postIDs,
		CurrentIndex: 0,
		TotalPosts:   len(resp.Posts),
		UserTgID:     targetTgID,
		Username:     userInfo.Username,
	}
	userPostView.Unlock()

	// Показываем первый пост
	showSingleUserPost(ctx, b, chatID, 0, userClient, targetTgID, 0)
}

// Функция для показа поста по ID (для callback-запросов)
func showPostByID(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient, postID string) {
	resp, err := userClient.GetPost(ctx, &pb.GetPostRequest{
		PostId: postID,
	})

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Пост не найден",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	created := formatDate(resp.Post.CreatedAt)

	text := fmt.Sprintf("📝 **Пост от @%s**\n\n", resp.Post.AuthorUsername)
	text += fmt.Sprintf("%s\n\n", resp.Post.Content)

	if resp.Post.MediaUrl != "" {
		text += fmt.Sprintf("📎 [Медиа](%s)\n", resp.Post.MediaUrl)
	}

	text += fmt.Sprintf("📅 %s\n", created)
	text += fmt.Sprintf("💬 Комментариев: %d\n\n", resp.Post.CommentsCount)

	if len(resp.Comments) > 0 {
		text += "**Комментарии:**\n"
		for _, comment := range resp.Comments {
			//if i >= 5 {
			//	text += "...\n"
			//	break
			//}
			commentDate := formatDate(comment.CreatedAt)
			text += fmt.Sprintf("@%s: %s _(%s)_\n",
				comment.AuthorUsername, comment.Content, commentDate)
		}
	}

	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "💬 Комментировать", CallbackData: "comment_" + postID},
			},
			{
				{Text: "🔙 Назад к списку", CallbackData: "back_to_posts"},
			},
		},
	}

	tgID := strconv.FormatInt(chatID, 10)
	if resp.Post.AuthorTgId == tgID {
		keyboard.InlineKeyboard = append([][]models.InlineKeyboardButton{
			{{Text: "🗑️ Удалить пост", CallbackData: "delete_post_" + postID}},
		}, keyboard.InlineKeyboard...)
	}

	// Отправляем как новое сообщение
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "Markdown",
		ReplyMarkup: keyboard,
	})
}

func truncateText(text string, maxLen int) string {
	text = sanitizeUTF8(text)
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}
func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "")
}

// Функция для пагинации постов пользователя (аналог handlePostsPagination)
//func handleUserPostsPagination(
//	ctx context.Context,
//	b *bot.Bot,
//	chatID int64,
//	messageID int,
//	userClient pb.UserServiceClient,
//	targetTgID string,
//	page int32,
//	callbackID string,
//) {
//
//	log.Printf("📰 handleUserPostsPagination: targetTgID=%s page=%d msg=%d",
//		targetTgID, page, messageID)
//
//	userInfo, err := userClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{
//		TgId: targetTgID,
//	})
//	if err != nil {
//		log.Println("ERR userInfo:", err)
//		return
//	}
//
//	resp, err := userClient.GetUserPosts(ctx, &pb.GetUserPostsRequest{
//		TgId:     targetTgID,
//		Page:     page,
//		PageSize: 5,
//	})
//	if err != nil {
//		log.Println("ERR posts:", err)
//		return
//	}
//
//	text := fmt.Sprintf("📰 **Посты пользователя @%s (страница %d):**\n\n",
//		userInfo.Username, page)
//
//	for i, post := range resp.Posts {
//		cleanContent := sanitizeUTF8(post.Content)
//		short := truncateText(cleanContent, 50)
//		num := (int(page)-1)*5 + i + 1
//		text += fmt.Sprintf(
//			"%d. **%s**\n   💬 %d\n   ID: `%s`\n\n",
//			num,
//			short,
//			post.CommentsCount,
//			post.PostId,
//		)
//	}
//	keyboard := &models.InlineKeyboardMarkup{
//		InlineKeyboard: [][]models.InlineKeyboardButton{{}},
//	}
//
//	if page > 1 {
//		keyboard.InlineKeyboard[0] = append(
//			keyboard.InlineKeyboard[0],
//			models.InlineKeyboardButton{
//				Text: "⬅️ Назад",
//				CallbackData: fmt.Sprintf(
//					"user_posts_prev_%s_%d",
//					targetTgID,
//					page-1,
//				),
//			})
//	}
//
//	if len(resp.Posts) == 5 {
//		keyboard.InlineKeyboard[0] = append(
//			keyboard.InlineKeyboard[0],
//			models.InlineKeyboardButton{
//				Text: "➡️ Вперед",
//				CallbackData: fmt.Sprintf(
//					"user_posts_next_%s_%d",
//					targetTgID,
//					page+1,
//				),
//			})
//	}
//
//	keyboard.InlineKeyboard = append(
//		keyboard.InlineKeyboard,
//		[]models.InlineKeyboardButton{
//			{Text: "🔙 Назад к поиску", CallbackData: "back_to_search"},
//		},
//	)
//
//	if !utf8.ValidString(text) {
//		log.Println("TEXT NOT UTF8")
//	}
//
//	text = sanitizeUTF8(text)
//
//	_, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
//		ChatID:      chatID,
//		MessageID:   messageID,
//		Text:        text,
//		ParseMode:   "Markdown",
//		ReplyMarkup: keyboard,
//	})
//
//	if err != nil {
//		log.Println("EDIT FAIL:", err)
//	}
//}

func handleUsersPagination(ctx context.Context, b *bot.Bot, chatID int64, messageID int, userClient pb.UserServiceClient, data string) {
	tgID := strconv.FormatInt(chatID, 10)

	// Проверяем права админа
	adminResp, err := userClient.IsAdmin(ctx, &pb.IsAdminRequest{
		TgID: tgID,
	})

	if err != nil || !adminResp.IsAdmin {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: data,
			Text:            "⛔ Только для администраторов",
			ShowAlert:       true,
		})
		return
	}

	md := metadata.New(map[string]string{"tg_id": tgID})
	ctxWithMeta := metadata.NewOutgoingContext(ctx, md)

	userPages.RLock()
	currentPage := userPages.m[chatID]
	if currentPage == 0 {
		currentPage = 1
	}
	userPages.RUnlock()

	newPage := currentPage
	if strings.Contains(data, "next") {
		newPage = currentPage + 1
	} else if strings.Contains(data, "prev") && currentPage > 1 {
		newPage = currentPage - 1
	}

	userPages.Lock()
	userPages.m[chatID] = newPage
	userPages.Unlock()

	resp, err := userClient.ListUsers(ctxWithMeta, &pb.ListUsersRequest{
		Page:     int32(newPage),
		PageSize: 5,
	})

	if err != nil {
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "❌ Ошибка загрузки",
		})
		return
	}

	if len(resp.User) == 0 {
		if newPage > currentPage {
			newPage = currentPage
			userPages.Lock()
			userPages.m[chatID] = newPage
			userPages.Unlock()
			b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
				CallbackQueryID: data,
				Text:            "📭 Это последняя страница",
			})
			return
		}
	}

	text := fmt.Sprintf("📊 **Список пользователей (страница %d):**\n\n", newPage)
	for i, user := range resp.User {
		num := (newPage-1)*5 + i + 1
		text += fmt.Sprintf("%d. **%s** (@%s)\n    ID: `%s`\n\n",
			num, user.Name, user.Username, user.UserId)
	}

	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{}},
	}

	if newPage > 1 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{Text: "⬅️ Назад", CallbackData: "users_page_prev"})
	}

	if len(resp.User) == 5 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{Text: "➡️ Вперед", CallbackData: "users_page_next"})
	}

	b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   messageID,
		Text:        text,
		ParseMode:   "Markdown",
		ReplyMarkup: keyboard,
	})
}

func handlePostsPagination(ctx context.Context, b *bot.Bot, chatID int64, messageID int, userClient pb.UserServiceClient, data string) {
	// Извлекаем номер страницы из данных
	var page int32 = 1

	userPages.RLock()
	currPage := userPages.m[chatID]
	userPages.RUnlock()
	if currPage == 0 {
		currPage = 1
	}

	newPage := currPage
	if strings.Contains(data, "next") {
		newPage = currPage + 1
	} else if strings.Contains(data, "prev") && currPage > 1 {
		newPage = currPage - 1
	}

	userPages.Lock()
	userPages.m[chatID] = newPage
	userPages.Unlock()

	// Правильно парсим данные: "posts_page_next_2" или "posts_page_prev_1"
	parts := strings.Split(data, "_")
	log.Printf("🔍 Parsed parts: %v", parts)
	if len(parts) >= 4 {
		// Определяем направление
		if parts[2] == "next" || parts[2] == "prev" {
			p, err := strconv.Atoi(parts[3])
			if err == nil {
				page = int32(p)
				log.Printf("✅ Parsed page: %d", page)
			} else {
				log.Printf("❌ Failed to parse page number from %s: %v", parts[3], err)
			}

		}
	}

	tgID := strconv.FormatInt(chatID, 10)
	log.Printf("🔍 Getting posts for tgID=%s, page=%d", tgID, page)

	resp, err := userClient.GetUserPosts(ctx, &pb.GetUserPostsRequest{
		TgId:     tgID,
		Page:     page,
		PageSize: 5,
	})

	if err != nil {
		log.Printf("❌ GetUserPosts error: %v", err)

		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "❌ Ошибка загрузки постов",
		})
		return
	}
	log.Printf("✅ Got %d posts for page %d", len(resp.Posts), page)

	if len(resp.Posts) == 0 {
		if newPage > currPage {
			// Возвращаем старую страницу
			userPages.Lock()
			userPages.m[chatID] = currPage
			userPages.Unlock()

			b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
				CallbackQueryID: data,
				Text:            "📭 Это последняя страница",
				ShowAlert:       false,
			})
			return
		}
	}

	text := fmt.Sprintf("📰 **Мои посты (страница %d):**\n\n", page)
	for i, post := range resp.Posts {
		created := formatDate(post.CreatedAt)
		previewText := post.Content
		if len(previewText) > 50 {
			previewText = previewText[:50] + "..."
		}
		num := (newPage-1)*5 + i + 1
		text += fmt.Sprintf("%d. **%s**\n   💬 Комментариев: %d\n   📅 %s\n   ID: `%s`\n\n",
			num, truncateText(post.Content, 50), post.CommentsCount, created, post.PostId)
	}

	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{}},
	}

	if page > 1 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{Text: "⬅️ Назад", CallbackData: fmt.Sprintf("posts_page_prev_%d", page-1)})
	}

	if len(resp.Posts) == 5 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{Text: "➡️ Вперед", CallbackData: fmt.Sprintf("posts_page_next_%d", page+1)})
	}

	log.Printf("📤 Sending updated message with %d keyboard buttons", len(keyboard.InlineKeyboard[0]))

	_, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   messageID,
		Text:        text,
		ParseMode:   "Markdown",
		ReplyMarkup: keyboard,
	})

	if err != nil {
		log.Printf("❌ Failed to edit message: %v", err)
	}
}
func handleFeedPagination(ctx context.Context, b *bot.Bot, chatID int64, messageID int, userClient pb.UserServiceClient, data string) {
	var page int32 = 1
	if strings.Contains(data, "next") {
		parts := strings.Split(data, "_")
		if len(parts) >= 4 {
			p, _ := strconv.Atoi(parts[3])
			page = int32(p)
		}
	} else if strings.Contains(data, "prev") {
		parts := strings.Split(data, "_")
		if len(parts) >= 4 {
			p, _ := strconv.Atoi(parts[3])
			page = int32(p)
		}
	}

	tgID := strconv.FormatInt(chatID, 10)

	userResp, err := userClient.GetUserByTgID(ctx, &pb.GetUserByTgIDRequest{
		TgId: tgID,
	})

	if err != nil {
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "❌ Пользователь не найден",
		})
		return
	}

	resp, err := userClient.GetFeed(ctx, &pb.GetFeedRequest{
		UserId:   userResp.UserId,
		Page:     page,
		PageSize: 5,
	})

	if err != nil {
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "❌ Ошибка загрузки ленты",
		})
		return
	}

	text := fmt.Sprintf("📊 **Лента (страница %d):**\n\n", page)
	for i, post := range resp.Posts {
		created := formatDate(post.CreatedAt)
		text += fmt.Sprintf("%d. **@%s**: %s\n   📅 %s\n   ID: `%s`\n\n",
			i+1, post.AuthorUsername, truncateText(post.Content, 100), created, post.PostId)
	}

	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{}},
	}

	if page > 1 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{Text: "⬅️ Назад", CallbackData: fmt.Sprintf("feed_page_prev_%d", page-1)})
	}

	if len(resp.Posts) == 5 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{Text: "➡️ Вперед", CallbackData: fmt.Sprintf("feed_page_next_%d", page+1)})
	}

	b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   messageID,
		Text:        text,
		ParseMode:   "Markdown",
		ReplyMarkup: keyboard,
	})
}

// ===== ПОКАЗ СПИСКА ПОЛЬЗОВАТЕЛЕЙ (НОВОЕ СООБЩЕНИЕ) =====
func showUserList(ctx context.Context, b *bot.Bot, chatID int64, userClient pb.UserServiceClient, page int32) {
	tgID := strconv.FormatInt(chatID, 10)

	// Сначала проверяем, является ли пользователь админом
	adminResp, err := userClient.IsAdmin(ctx, &pb.IsAdminRequest{
		TgID: tgID,
	})

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Ошибка проверки прав доступа",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	if !adminResp.IsAdmin {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "⛔ Эта функция доступна только администраторам",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	md := metadata.New(map[string]string{"tg_id": tgID})
	ctxWithMeta := metadata.NewOutgoingContext(ctx, md)

	userPages.Lock()
	userPages.m[chatID] = int(page)
	userPages.Unlock()

	resp, err := userClient.ListUsers(ctxWithMeta, &pb.ListUsersRequest{
		Page:     page,
		PageSize: 5,
	})

	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Ошибка загрузки: " + err.Error(),
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	if len(resp.User) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "📭 Список пользователей пуст",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	text := fmt.Sprintf("📊 **Список пользователей (страница %d):**\n\n", page)
	for i, user := range resp.User {
		num := (int(page)-1)*5 + i + 1
		text += fmt.Sprintf("%d. **%s** (@%s)\n    ID: `%s`",
			num, user.Name, user.Username, user.UserId)
	}

	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{},
		},
	}

	if page > 1 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{Text: "⬅️ Назад", CallbackData: "users_page_prev"})
	}

	if len(resp.User) == 5 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{Text: "➡️ Вперед", CallbackData: "users_page_next"})
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "Markdown",
		ReplyMarkup: keyboard,
	})
}

func showSingleUserPost(ctx context.Context, b *bot.Bot, chatID int64, messageID int, userClient pb.UserServiceClient, targetTgID string, index int) {
	key := fmt.Sprintf("%d_%s", chatID, targetTgID)

	userPostView.RLock()
	state, exists := userPostView.m[key]
	userPostView.RUnlock()

	if !exists || index < 0 || index >= len(state.PostIDs) {
		log.Printf("❌ Invalid post index or state not found")
		return
	}

	// Обновляем индекс
	state.CurrentIndex = index

	// Получаем пост по ID
	postID := state.PostIDs[index]
	resp, err := userClient.GetPost(ctx, &pb.GetPostRequest{
		PostId: postID,
	})
	if err != nil {
		log.Printf("❌ Failed to get post: %v", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        "❌ Ошибка загрузки поста",
			ReplyMarkup: getMainKeyboard(),
		})
		return
	}

	// Формируем текст поста
	created := formatDate(resp.Post.CreatedAt)
	text := fmt.Sprintf("📝 **Пост @%s** (%d из %d)\n\n",
		state.Username, index+1, state.TotalPosts)
	text += fmt.Sprintf("%s\n\n", resp.Post.Content)

	if resp.Post.MediaUrl != "" {
		text += fmt.Sprintf("📎 [Медиа](%s)\n", resp.Post.MediaUrl)
	}

	text += fmt.Sprintf("📅 %s\n", created)
	text += fmt.Sprintf("💬 Комментариев: %d\n\n", resp.Post.CommentsCount)

	if len(resp.Comments) > 0 {
		text += "**Комментарии:**\n"
		for _, comment := range resp.Comments {
			//if i >= 3 { // Показываем только первые 3 комментария
			//	text += "...\n"
			//	break
			//}
			commentDate := formatDate(comment.CreatedAt)
			text += fmt.Sprintf("@%s: %s _(%s)_\n",
				comment.AuthorUsername, comment.Content, commentDate)
		}
	}

	// Создаем клавиатуру для навигации
	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{}, // Первая строка - навигация
			{}, // Вторая строка - действия
			{}, // Третья строка - назад к поиску
		},
	}

	// Кнопки навигации (первая строка)
	if index > 0 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{
				Text:         "⬅️ Предыдущий",
				CallbackData: fmt.Sprintf("user_post_nav_%s_prev_%d", targetTgID, index-1),
			})
	}

	if index < state.TotalPosts-1 {
		keyboard.InlineKeyboard[0] = append(keyboard.InlineKeyboard[0],
			models.InlineKeyboardButton{
				Text:         "Следующий ➡️",
				CallbackData: fmt.Sprintf("user_post_nav_%s_next_%d", targetTgID, index+1),
			})
	}

	// Кнопки действий (вторая строка)
	keyboard.InlineKeyboard[1] = append(keyboard.InlineKeyboard[1],
		models.InlineKeyboardButton{
			Text:         "💬 Комментировать",
			CallbackData: "comment_" + postID,
		})

	// Кнопка "Назад к поиску" (третья строка)
	keyboard.InlineKeyboard[2] = append(keyboard.InlineKeyboard[2],
		models.InlineKeyboardButton{
			Text:         "🔙 Назад к поиску",
			CallbackData: "back_to_search",
		})

	// Если это наш пост, добавляем кнопку удаления
	tgID := strconv.FormatInt(chatID, 10)
	if resp.Post.AuthorTgId == tgID {
		// Вставляем кнопку удаления в начало второй строки
		deleteBtn := []models.InlineKeyboardButton{
			{Text: "🗑️ Удалить", CallbackData: "delete_post_" + postID},
		}
		keyboard.InlineKeyboard[1] = append(deleteBtn, keyboard.InlineKeyboard[1]...)
	}

	if messageID > 0 {
		// Редактируем существующее сообщение
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   "Markdown",
			ReplyMarkup: keyboard,
		})
	} else {
		// Отправляем новое (первый раз)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        text,
			ParseMode:   "Markdown",
			ReplyMarkup: keyboard,
		})
	}

}
