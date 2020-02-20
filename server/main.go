// Copyright (c) 2020 Manikandan Sundararajan. All Rights Reserved.
// See License.txt for license information.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"regexp"
	"strings"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

const (
	sampleName = "Tally Bot"
	botName    = "tallybot"

	teamName       = "botteam"
	channelLogName = "debugging-for-sample-bot"
)

type tokens struct {
	MattermostID     string `json:"mattermost_id"`
	MattermostSecret string `json:"mattermost_secret"`
	GithubToken      string `json:"github_token"`
}

var client *model.Client4
var webSocketClient *model.WebSocketClient
var accessTokens tokens

var githubClient *githubv4.Client

var botUser *model.Bot
var botTeam *model.Team
var debuggingChannel *model.Channel

// Documentation for the Go driver can be found
// at https://godoc.org/github.com/mattermost/platform/model#Client
func main() {
	println(sampleName)

	setupGracefulShutdown()

	client = model.NewAPIv4Client("http://localhost:8065")
	// accessToken = tokens{ID: "44ri3z5fgtrabbxrmwjw4wobwh", Secret: "agfbesc1ifyhbkw8frt717s58h"}
	accessTokens = getTokens()
	// This will set the token required for all future calls
	// You can get this token with client.AuthToken
	client.SetToken(accessTokens.MattermostSecret)

	// Lets test to see if the mattermost server is up and running
	makeSureServerIsRunning()
	authenticateToGithub()

	// lets attempt to login to the Mattermost server as the bot user
	// loginAsTheBotUser()
	getTheBot(botName)

	// Lets find our bot team
	findBotTeam()

	// This is an important step.  Lets make sure we use the botTeam
	// for all future web service requests that require a team.
	//client.SetTeamId(botTeam.Id)

	// Lets create a bot channel for logging debug messages into
	createBotDebuggingChannelIfNeeded()
	sendMsgToDebuggingChannel("_"+sampleName+" has **started** running_", "")

	// Lets start listening to some channels via the websocket!
	webSocketClient, err := model.NewWebSocketClient4("ws://localhost:8065", client.AuthToken)
	if err != nil {
		println("We failed to connect to the web socket")
		printError(err)
	}

	webSocketClient.Listen()

	go func() {
		for {
			select {
			case resp := <-webSocketClient.EventChannel:
				handleWebSocketResponse(resp)
			}
		}
	}()

	// You can block forever with
	select {}
}

func makeSureServerIsRunning() {
	if props, resp := client.GetOldClientConfig(""); resp.Error != nil {
		println("There was a problem pinging the Mattermost server.  Are you sure it's running?")
		printError(resp.Error)
		os.Exit(1)
	} else {
		println("Server detected and is running version " + props["Version"])
	}
}

func authenticateToGithub() {
	ctx := context.Background()
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: accessTokens.GithubToken},
	)
	httpClient := oauth2.NewClient(ctx, src)

	githubClient = githubv4.NewClient(httpClient)
}

func getTokens() tokens {
	jsonFile, err := os.Open("../config.json")

	if err != nil {
		println("There was a problem opening the config file. Are you sure you ran the setup steps from the README.md")
		println(err.Error)
		os.Exit(1)
	}

	defer jsonFile.Close() // defer the closing of the JSON file until the end of the func
	byteValue, _ := ioutil.ReadAll(jsonFile)

	var configTokens tokens

	json.Unmarshal(byteValue, &configTokens)
	return configTokens
}

func getTheBot(botUserName string) {
	listBots, resp := client.GetBots(0, 100, botUserName)
	if resp.Error != nil {
		println("There was a problem retrieving the list of bots. Are you sure you ran the setup steps from the README.md")
		printError(resp.Error)
		os.Exit(1)
	}

	for _, bot := range listBots {
		if bot.Username == botName {
			botUser = bot
		}
	}

	if botUser == nil {
		println("Could not find the specified Bot. Are you sure you ran the setup steps from the README.md")
		printError(resp.Error)
		os.Exit(1)
	}
}

// func loginAsTheBotUser() {
// 	if user, resp := client.Login(botEmail, botPassword); resp.Error != nil {
// 		println("There was a problem logging into the Mattermost server.  Are you sure ran the setup steps from the README.md?")
// 		printError(resp.Error)
// 		os.Exit(1)
// 	} else {
// 		botUser = user
// 	}
// }

// func updateTheBotUserIfNeeded() {
// 	if botUser.FirstName != userFirst || botUser.LastName != userLast || botUser.Username != botName {
// 		botUser.FirstName = userFirst
// 		botUser.LastName = userLast
// 		botUser.Username = botName

// 		if user, resp := client.UpdateUser(botUser); resp.Error != nil {
// 			println("We failed to update the Sample Bot user")
// 			printError(resp.Error)
// 			os.Exit(1)
// 		} else {
// 			botUser = user
// 			println("Looks like this might be the first run so we've updated the bots account settings")
// 		}
// 	}
// }

func findBotTeam() {
	if team, resp := client.GetTeamByName(teamName, ""); resp.Error != nil {
		println("We failed to get the initial load")
		println("or we do not appear to be a member of the team '" + teamName + "'")
		printError(resp.Error)
		os.Exit(1)
	} else {
		botTeam = team
	}
}

func createBotDebuggingChannelIfNeeded() {
	if rchannel, resp := client.GetChannelByName(channelLogName, botTeam.Id, ""); resp.Error != nil {
		println("We failed to get the channels")
		printError(resp.Error)
	} else {
		debuggingChannel = rchannel
		return
	}

	// Looks like we need to create the logging channel
	channel := &model.Channel{}
	channel.Name = channelLogName
	channel.DisplayName = "Debugging For Sample Bot"
	channel.Purpose = "This is used as a test channel for logging bot debug messages"
	channel.Type = model.CHANNEL_OPEN
	channel.TeamId = botTeam.Id
	if rchannel, resp := client.CreateChannel(channel); resp.Error != nil {
		println("We failed to create the channel " + channelLogName)
		printError(resp.Error)
	} else {
		debuggingChannel = rchannel
		println("Looks like this might be the first run so we've created the channel " + channelLogName)
	}
}

func sendMsgToDebuggingChannel(msg string, replyToID string) {
	post := &model.Post{}
	post.ChannelId = debuggingChannel.Id
	post.Message = msg

	post.RootId = replyToID

	if _, resp := client.CreatePost(post); resp.Error != nil {
		println("We failed to send a message to the logging channel")
		printError(resp.Error)
	}
}

func handleWebSocketResponse(event *model.WebSocketEvent) {
	handleMsgFromDebuggingChannel(event)
}

func handleMsgFromDebuggingChannel(event *model.WebSocketEvent) {
	// If this isn't the debugging channel then lets ingore it
	if event.Broadcast.ChannelId != debuggingChannel.Id {
		return
	}

	// Lets only reponded to messaged posted events
	if event.Event != model.WEBSOCKET_EVENT_POSTED {
		return
	}

	// if event.Event == model.WEBSOCKET_EVENT_NEW_USER {

	// }

	println("responding to debugging channel msg")

	post := model.PostFromJson(strings.NewReader(event.Data["post"].(string)))
	if post != nil {

		// ignore my events
		if post.UserId == botUser.UserId {
			return
		}

		// if you see any word matching 'alive' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)alive(?:$|\W)`, post.Message); matched {
			sendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'up' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)up(?:$|\W)`, post.Message); matched {
			sendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'running' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)running(?:$|\W)`, post.Message); matched {
			sendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'hello' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)hello(?:$|\W)`, post.Message); matched {
			sendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching github, then respond with user's github username
		if matched, _ := regexp.MatchString(`(?:^|\W)github(?:$|\W)`, post.Message); matched {
			gitHubUsername := whatsMyGithub()
			sendMsgToDebuggingChannel(gitHubUsername, post.Id)
			return
		}
	}

	sendMsgToDebuggingChannel("I did not understand you!", post.Id)
}

// func authToGithub() {
// 	ctx := context.Background()
// 	ts := oauth2.StaticTokenSource(
// 		&oauth2.Token{AccessToken: "... your access token ..."},
// 	)
// 	tc := oauth2.NewClient(ctx, ts)

// 	client := github.NewClient(tc)

// 	// list all repositories for the authenticated user
// 	repos, _, err := client.Repositories.List(ctx, "", nil)
// }

// This won't work unless the GitHub plugin exists and is configured
func whatsMyGithub() string {
	listCommands, resp := client.ListAutocompleteCommands(botTeam.Id)
	if resp.StatusCode == 0 {
		return "Failed to get GitHub username"
	}

	for _, c := range listCommands {
		fmt.Println(c.DisplayName)
		if c.DisplayName == "github" {
			cmd, resp := client.ExecuteCommand(debuggingChannel.Id, " /github me")

			if resp.StatusCode == 0 {
				return "Failed to get GitHub Username"
			}

			return cmd.Text
		}
	}

	return "Failed to get Github username"
}

func printError(err *model.AppError) {
	println("\tError Details:")
	println("\t\t" + err.Message)
	println("\t\t" + err.Id)
	println("\t\t" + err.DetailedError)
}

func setupGracefulShutdown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			if webSocketClient != nil {
				webSocketClient.Close()
			}

			sendMsgToDebuggingChannel("_"+sampleName+" has **stopped** running_", "")
			os.Exit(0)
		}
	}()
}
