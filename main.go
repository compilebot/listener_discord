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
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gopherpun/redis_queue"
	"github.com/sirupsen/logrus"
)

// Env Variables
var (
	Token            string
	RedisHost        string
	JobQueueKey      string
	ResponseQueueKey string
	JobQueue         *redis_queue.Queue
	ResponseQueue    *redis_queue.Queue
	requests         map[string]*discordgo.Session
	service          string
)

func init() {
	Token = os.Getenv("DISCORD_TOKEN")
	RedisHost = os.Getenv("REDIS_HOST")
	JobQueueKey = os.Getenv("JOB_QUEUE")
	ResponseQueueKey = os.Getenv("RESPONSE_QUEUE")

	service = "discord_listener"

	logrus.SetFormatter(&logrus.JSONFormatter{})

	jq, err := redis_queue.NewQueue(RedisHost, JobQueueKey)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"msg":     "Failed to connect to Redis.",
			"service": service,
			"err_msg": err,
		}).Fatal()
		panic(err)
	}
	rq, err := redis_queue.NewQueue(RedisHost, ResponseQueueKey)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"msg":     "Failed to connect to Redis.",
			"service": service,
		}).Fatal()
		panic(err)
	}

	JobQueue = jq
	ResponseQueue = rq
	ResponseQueue.Conn.Do("FLUSHALL")

	// TODO don't violate 12 factor with sticky sessions
	requests = make(map[string]*discordgo.Session)
}

func main() {

	// Create a new Discord session using the provided bot token.
	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"msg":     "Failed to create Discord session with associated token.",
			"service": service,
			"err_msg": err,
		}).Error()
		return
	}

	// Register the messageCreate func as a callback for MessageCreate events.
	dg.AddHandler(messageCreate)

	// Open a websocket connection to Discord and begin listening.
	err = dg.Open()
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"msg":     "Failed to connect to Discord.",
			"service": service,
			"err_msg": err,
		}).Fatal()
		fmt.Println("error opening connection,", err)
		return
	}

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	go pollQueue()
	<-sc

	// Cleanly close down the Discord session.
	dg.Close()
}

func pollQueue() {
	rate := time.Second
	throttle := time.Tick(rate)
	for {
		<-throttle
		go func() {

			// TODO
			ready, err := ResponseQueue.Peek()

			if !ready {
				return
			}

			item, err := ResponseQueue.Dequeue()

			if err != nil {
				logrus.WithFields(logrus.Fields{
					"msg":     "Failed to dequeue item from Response Queue.",
					"service": service,
					"err_msg": err,
				}).Error()
				return
			}

			response := decodeResponse(item)

			requests[response.RequestID].ChannelMessageSend(response.ChannelID, fmt.Sprintf("Output for: %s\n```%s```", response.RequestID, response.Response))
		}()
	}
}

// Response is a JSON struct represention information about the response.
type Response struct {
	ChannelID string
	Code      string
	Language  string
	RequestID string
	Response  string
}

func decodeResponse(responseItem string) Response {
	var response Response
	_ = json.Unmarshal([]byte(responseItem), &response)

	return response
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the autenticated bot has access to.
func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	langs := map[string]bool{"go": true, "python": true}

	// Ignore all messages created by the bot itself
	if m.Author.ID == s.State.User.ID {
		return
	}

	if len(m.Content) < 11 || m.Content[:11] != "+compilebot" {
		return
	}

	valid, err := validCommand(m.Content)

	if err != nil {
		logrus.WithFields(logrus.Fields{
			"msg":     "Error parsing command.",
			"service": service,
			"err_msg": err,
		}).Warn()
		s.ChannelMessageSend(m.ChannelID, "Error parsing command.")
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
		logrus.WithFields(logrus.Fields{
			"msg":     "Error parsing code.",
			"service": service,
			"err_msg": err,
		}).Warn()
		s.ChannelMessageSend(m.ChannelID, "Error parsing code.")
	}

	requestID := randomString(10)
	requests[requestID] = s
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Working on request: %s", requestID))

	code = strings.Trim(code, "`")
	json, err := encodeJob(requestID, m.ChannelID, code, lang)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"msg":     "Error encoding job.",
			"service": service,
			"err_msg": err,
		}).Warn()
		s.ChannelMessageSend(m.ChannelID, "Error: EncodeJob.")
		return
	}

	err = JobQueue.Enqueue(json)

	if err != nil {
		logrus.WithFields(logrus.Fields{
			"msg":     "Error adding job to Job Queue.",
			"service": service,
			"err_msg": err,
		}).Warn()
		s.ChannelMessageSend(m.ChannelID, "Error: JobQueue.")
		return
	}
}

// Job is a JSON structure representing information about the job.
type Job struct {
	ChannelID string `json:"channelID"`
	Code      string `json:"code"`
	Language  string `json:"language"`
	RequestID string `json:"requestID"`
}

func encodeJob(requestID, channelID, code, lang string) (string, error) {
	jsonJob, err := json.Marshal(Job{channelID, code, lang, requestID})

	if err != nil {
		return "", err
	}

	return string(jsonJob), nil
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
