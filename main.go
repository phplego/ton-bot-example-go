package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/tucnak/telebot"
	_ "modernc.org/sqlite" // SQLite driver
)

// Config variables
var (
	BotToken       = os.Getenv("TELEGRAM_BOT_TOKEN")
	APIKey         = os.Getenv("TONCENTER_API_KEY")
	DepositAddress = os.Getenv("DEPOSIT_ADDRESS")
	APIBaseURL     = "https://toncenter.com/api/v2"
	DBPath         = "./db.sqlite"
)

// Global variables
var (
	db  *sql.DB
	bot *telebot.Bot
)

// Initialize the SQLite database
func initDB() {
	var err error
	db, err = sql.Open("sqlite", DBPath)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	createTable := `
	CREATE TABLE IF NOT EXISTS Users (
		uid INTEGER PRIMARY KEY,
		balance INTEGER DEFAULT 0
	)`
	_, err = db.Exec(createTable)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}
}

// Check if user exists in the database
func checkUser(uid int) bool {
	row := db.QueryRow("SELECT 1 FROM Users WHERE uid = ?", uid)
	var exists int
	err := row.Scan(&exists)
	return err == nil
}

// Add a new user to the database
func addUser(uid int) {
	_, err := db.Exec("INSERT INTO Users (uid) VALUES (?)", uid)
	if err != nil {
		log.Printf("Failed to add user %d: %v", uid, err)
	}
}

// Get the balance of a user
func getBalance(uid int) int {
	row := db.QueryRow("SELECT balance FROM Users WHERE uid = ?", uid)
	var balance int
	err := row.Scan(&balance)
	if err != nil {
		log.Printf("Failed to get balance for user %d: %v", uid, err)
		return 0
	}
	return balance
}

// Add balance to a user
func addBalance(uid int, amount int) {
	_, err := db.Exec("UPDATE Users SET balance = balance + ? WHERE uid = ?", amount, uid)
	if err != nil {
		log.Printf("Failed to add balance for user %d: %v", uid, err)
	}
}

// Start checking for new deposits
func startDepositChecker() {
	lastLT := loadLastLT()

	go func() {
		for {
			time.Sleep(2 * time.Second)

			resp, err := http.Get(fmt.Sprintf("%s/getTransactions?address=%s&limit=100&archival=true&api_key=%s",
				APIBaseURL, DepositAddress, APIKey))
			if err != nil {
				log.Printf("Failed to fetch transactions: %v", err)
				continue
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			var result map[string]any
			if err := json.Unmarshal(body, &result); err != nil {
				log.Printf("Failed to parse response: %v", err)
				continue
			}

			for _, tx := range result["result"].([]any) {
				transaction := tx.(map[string]any)
				lt, err := strconv.Atoi(transaction["transaction_id"].(map[string]any)["lt"].(string))
				if err != nil {
					log.Printf("Failed to parse LT: %v", err)
					continue
				}

				if lt <= lastLT {
					continue
				}

				value, err := strconv.Atoi(transaction["in_msg"].(map[string]any)["value"].(string))
				uid, ok := transaction["in_msg"].(map[string]any)["message"].(string)
				if value > 0 && ok {
					if userID, err := strconv.Atoi(uid); err == nil && checkUser(userID) {
						addBalance(userID, value)
						bot.Send(&telebot.User{ID: userID}, fmt.Sprintf("Deposit confirmed!\n*+%.2f TON*", float64(value)/1e9), telebot.ModeMarkdown)
					}
				}

				lastLT = lt
				saveLastLT(lastLT)
			}
		}
	}()
}

// Load last processed LT
func loadLastLT() int {
	data, err := os.ReadFile("last_lt.txt")
	if err != nil {
		return 0
	}
	lt, _ := strconv.Atoi(string(data))
	return lt
}

// Save last processed LT
func saveLastLT(lt int) {
	os.WriteFile("last_lt.txt", []byte(strconv.Itoa(lt)), 0644)
}

func main() {
	log.Println("Starting bot...")
	// Initialize database
	initDB()

	// Initialize Telegram bot
	var err error
	bot, err = telebot.NewBot(telebot.Settings{
		Token:  BotToken,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	// Handle /start command
	bot.Handle("/start", func(m *telebot.Message) {
		if !checkUser(m.Sender.ID) {
			addUser(m.Sender.ID)
		}
		bot.Send(m.Sender, "Welcome! Use /balance to check your balance or /deposit to top up your account.")
	})

	// Handle /balance command
	bot.Handle("/balance", func(m *telebot.Message) {
		balance := getBalance(m.Sender.ID)
		bot.Send(m.Sender, fmt.Sprintf("Your balance is: %.2f TON", float64(balance)/1e9))
	})

	// Handle /deposit command
	bot.Handle("/deposit", func(m *telebot.Message) {
		bot.Send(m.Sender,
			fmt.Sprintf("Send TON to this address:\n%s\nInclude this comment: %d", DepositAddress, m.Sender.ID),
			&telebot.ReplyMarkup{
				InlineKeyboard: [][]telebot.InlineButton{
					{
						{Text: "Deposit", URL: fmt.Sprintf("ton://transfer/%s&text=%d", DepositAddress, m.Sender.ID)},
					},
				},
			},
		)
	})

	// Start deposit checker
	startDepositChecker()

	// Start the bot
	log.Println("Bot is running...")
	bot.Start()
}
