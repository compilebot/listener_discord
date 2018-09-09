package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/gopherpun/redis_queue"
)

// Env Variables
var (
	Token            string
	RedisHost        string
	JobQueueKey      string
	ResponseQueueKey string
	JobQueue         *redis_queue.Queue
	ResponseQueue    *redis_queue.Queue
)

func init() {
	Token = os.Getenv("DISCORD_TOKEN")
	RedisHost = os.Getenv("REDIS_HOST")
	JobQueueKey = os.Getenv("JOB_QUEUE")

	jq, err := redis_queue.NewQueue(RedisHost, JobQueueKey)
	if err != nil {
		panic(err)
	}
	rq, err := redis_queue.NewQueue(RedisHost, ResponseQueueKey)
	if err != nil {
		panic(err)
	}

	JobQueue = jq
	ResponseQueue = rq
}

func main() {

	// Create a new Discord session using the provided bot token.
	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		fmt.Println("error creating Discord session,", err)
		return
	}

	// Register the messageCreate func as a callback for MessageCreate events.
	dg.AddHandler(messageCreate)

	// Open a websocket connection to Discord and begin listening.
	err = dg.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	// Cleanly close down the Discord session.
	dg.Close()
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the autenticated bot has access to.
func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	langs := map[string]bool{"go": true}

	// Ignore all messages created by the bot itself
	if m.Author.ID == s.State.User.ID {
		return
	}

	if len(m.Content) < 11 || m.Content[:11] != "+compilebot" {
		return
	}

	valid, err := validCommand(m.Content)

	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("An error occured: %x", err))
		return
	}

	if !valid {
		s.ChannelMessageSend(m.ChannelID, "Invalid syntax. +compilebot <language> \\`\\`\\`<code>\\`\\`\\`")
		return
	}

	lang := strings.Split(m.Content, " ")[1]

	if _, ok := langs[lang]; !ok {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s not supported.", lang))
		return
	}

	code, err := getCode(m.Content)

	if err != nil {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("An error occured: %v", err))
	}

	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Working on request: %s", randomString(10)))

	code = strings.Trim(code, "`")
	err = JobQueue.Enqueue(encodeJob(s, m.ChannelID, code, lang))

	if err != nil {
		s.ChannelMessageSend(m.ChannelID, "Error: "+err.Error())
		return
	}
}

// Job is a JSON structure representing information about the job.
type Job struct {
	Session   *discordgo.Session `json:"session"`
	ChannelID string             `json:"channelID"`
	Code      string             `json:"code"`
	Language  string             `json:"language"`
}

func encodeJob(s *discordgo.Session, channelID, code, lang string) string {
	jsonJob, _ := json.Marshal(Job{s, channelID, code, lang})

	fmt.Println(jsonJob)
	return string(jsonJob)
}

func validCommand(cmd string) (matched bool, err error) {
	matched, err = regexp.MatchString(`(?ms)^\+compilebot .[a-z]* \x60{3}.*\x60{3}$`, cmd)
	return
}

func getCode(cmd string) (code string, err error) {
	r, err := regexp.Compile(`(?ms)\x60{3}.*\x60{3}$`)
	if err != nil {
		return
	}
	code = string(r.Find([]byte(cmd)))
	return
}

func randomString(l int) string {
	bytes := make([]byte, l)
	for i := 0; i < l; i++ {
		bytes[i] = byte(randInt(97, 122))
	}
	return string(bytes)
}

func randInt(min int, max int) int {
	return min + rand.Intn(max-min)
}
