package main

// Imports
import (
	"fmt"
	"os"
	"time"
	"math"
	"syscall"
	"context"
	"strconv"
	"errors"
	"strings"
	"runtime"
	"bytes"
	"os/signal"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"

	"go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/bson" 
    "go.mongodb.org/mongo-driver/mongo/options"
)

// Types
type (
	customCommand struct {
		Group string
		Description string
		Options []*discordgo.ApplicationCommandOption
		Usage string
		Run func(bot *discordgo.Session, interaction *discordgo.InteractionCreate)
	}

	email struct {
		Author string
		Title string
		Recipients []string
		Content string
		Date string
	}

	twoFactor struct {
		Active bool
		Question string
		Answer string
	}
)

// Variables
var (
	embedColor = 0x2f3136
	database *mongo.Collection
	cooldowns = map[string]bool {}
	guildCount = 0
	userCount = 0
)

// Main Function
func main() {
	godotenv.Load()

	client, err := mongo.Connect(
		context.TODO(),
		options.Client().ApplyURI(os.Getenv("MongoURI")),
	)

	if err != nil {
		fmt.Println(err)
	}

	bot, err := discordgo.New(os.Getenv("BotToken"))

	if err != nil {
		fmt.Println(err)
	}

	bot.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMessageReactions |
		discordgo.IntentsGuildMembers

	database = client.Database("DiscordBots").Collection("EtsukoAccounts")

	bot.AddHandler(ready)
	bot.AddHandler(interactionCreate)
	bot.AddHandler(guildCreate)
	bot.AddHandler(guildDelete)
	bot.AddHandler(guildMemberAdd)
	bot.AddHandler(guildMemberRemove)

	err = bot.Open()

	if err != nil {
		fmt.Println(err)
	}

	exit := make(chan os.Signal, 1)
	signal.Notify(
		exit, 
		syscall.SIGINT, 
		syscall.SIGTERM, 
		os.Interrupt, 
		os.Kill,
	)

	<-exit
	bot.Close()
}

// Event Functions
func ready(bot *discordgo.Session, ready *discordgo.Ready) {
	commands := []*discordgo.ApplicationCommand {}

	for name, cmd := range listAppCommands() {
		commands = append(commands, &discordgo.ApplicationCommand {
			Name: name,
			Type: discordgo.ChatApplicationCommand,
			Description: cmd.Description,
			Options: cmd.Options,
		})
	}

	bot.ApplicationCommandBulkOverwrite(bot.State.User.ID, "", commands)
	bot.UpdateStreamingStatus(1, "/commands", "https://twitch.tv/etsukobot")
	fmt.Printf("Managing emails within %v guilds!", len(ready.Guilds))
}

func guildCreate(bot *discordgo.Session, guild *discordgo.GuildCreate) {
	guildCount++
	userCount += guild.MemberCount
}

func guildDelete(bot *discordgo.Session, guild *discordgo.GuildDelete) {
	guildCount--
	userCount -= guild.MemberCount
}

func guildMemberAdd(bot *discordgo.Session, guild *discordgo.GuildMemberAdd) {
	userCount++
}

func guildMemberRemove(bot *discordgo.Session, guild *discordgo.GuildMemberRemove) {
	userCount--
}

func interactionCreate(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction.Type == discordgo.InteractionApplicationCommand && interaction.GuildID != "" {
		name := interaction.ApplicationCommandData().Name
		cmd, valid := listAppCommands()[name]
		data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})

		if err == nil && valid {
			if _, isOnCooldown := cooldowns[interaction.Member.User.ID]; isOnCooldown {
				bot.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse {
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData {
						Flags: 1 << 6,
						Content: "You're on cooldown for `3s`!",
					},
				})

				return
			}

			if _, dataValid := data["UserID"]; !(!dataValid && !(map[string]bool {"signup": true, "login": true})[name]) {
				cmd.Run(bot, interaction)

				cooldowns[interaction.Member.User.ID] = true

				time.AfterFunc(time.Second * 3, func() {
					delete(cooldowns, interaction.Member.User.ID)
				})

				return
			}

			bot.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse {
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData {
					Flags: 1 << 6,
					Content: "Run `/signup` or `/login` first!",
				},
			})
		}
	}
}

// Mongo Functions
func findFromMongo(data bson.M) (bson.M, error) {
	var results bson.M

	err := database.FindOne(context.TODO(), data).Decode(&results)

	if err != nil && err != mongo.ErrNoDocuments {
		return results, err
	}

	return results, nil
}

func updateInMongo(action string, data bson.M, newValue bson.M) error {
	_, err := database.UpdateOne(context.TODO(), data, bson.M {action: newValue})

	return err
}

// Utility Functions
func listAppCommands() map[string]*customCommand {
	return map[string]*customCommand {
		"ping": &customCommand {
			Group: "Fun",
			Description: "Pong!",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				start := time.Now().UTC().UnixMilli()

				bot.InteractionRespond(
					interaction.Interaction,
					&discordgo.InteractionResponse {
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionResponseData {
							Flags: 1 << 1,
							Content: "Pinging...",
						},
					},
				)

				api := time.Now().UTC().UnixMilli() - start
				start = time.Now().UTC().UnixMilli()

				_, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})

				db := time.Now().UTC().UnixMilli() - start

				if err == nil {
					bot.InteractionResponseEdit(
						bot.State.User.ID,
						interaction.Interaction, 
						&discordgo.WebhookEdit {
							Content: strings.Join([]string {
								fmt.Sprintf("API Latency: `%vms`", api),
								fmt.Sprintf("Database Latency: `%vms`", db),
							}, "\n"),
						},
					)
				}
			},
		},
		"signup": &customCommand {
			Group: "Personal",
			Options: []*discordgo.ApplicationCommandOption {
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "username",
					Description: "The username for the account.",
					Required: true,
				},
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "password",
					Description: "The password for the account.",
					Required: true,
				},
			},
			Description: "Signs you up for my services.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {	
				data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})
				options := interaction.ApplicationCommandData().Options

				if err == nil {
					if _, valid := data["UserID"]; valid {
						bot.InteractionRespond(
							interaction.Interaction,
							&discordgo.InteractionResponse {
								Type: discordgo.InteractionResponseChannelMessageWithSource,
								Data: &discordgo.InteractionResponseData {
									Flags: 1 << 6,
									Content: "You've already signed up!",
								},
							},
						)

						return
					}

					username := options[0].StringValue()
					password := options[1].StringValue()
					data, err = findFromMongo(bson.M {"Username": username})

					if err == nil {
						if _, valid := data["UserID"]; valid {
							bot.InteractionRespond(
								interaction.Interaction,
								&discordgo.InteractionResponse {
									Type: discordgo.InteractionResponseChannelMessageWithSource,
									Data: &discordgo.InteractionResponseData {
										Flags: 1 << 6,
										Content: "That username is already under an account!",
									},
								},
							)

							return
						}

						if len(strings.Split(username, "")) > 25 {
							bot.InteractionRespond(
								interaction.Interaction,
								&discordgo.InteractionResponse {
									Type: discordgo.InteractionResponseChannelMessageWithSource,
									Data: &discordgo.InteractionResponseData {
										Flags: 1 << 6,
										Content: "The account username cannot be over `25` letters!",
									},
								},
							)

							return
						}

						if len(strings.Split(password, "")) < 8 || len(strings.Split(password, "")) > 32 {
							bot.InteractionRespond(
								interaction.Interaction,
								&discordgo.InteractionResponse {
									Type: discordgo.InteractionResponseChannelMessageWithSource,
									Data: &discordgo.InteractionResponseData {
										Flags: 1 << 6,
										Content: "The account password cannot be under `8` letters, or over `32` letters!",
									},
								},
							)

							return
						}

						_, err := database.InsertOne(context.TODO(), bson.M {
							"UserID": "",
							"Username": username,
							"Password": password,
							"2FA": twoFactor {
								Active: false,
								Question: "",
								Answer: "",
							},
							"SignUpDate": createDate(time.Now()),
							"SentEmails": []*email {},
							"InboxedEmails": []*email {},
							"DraftedEmails": []*email {},
							"ContactList": map[string]bool {},
							"BlockList": map[string]bool {},
							"ProtectInbox": true,
						})

						if err == nil {
							bot.InteractionRespond(
								interaction.Interaction,
								&discordgo.InteractionResponse {
									Type: discordgo.InteractionResponseChannelMessageWithSource,
									Data: &discordgo.InteractionResponseData {
										Flags: 1 << 6,
										Content: fmt.Sprintf("You've signed up as `@%v`, congrats! Now, run `/login`.", username),
									},
								},
							)
						}
					}
				}
			},
		},
		"login": &customCommand {
			Group: "Personal",
			Options: []*discordgo.ApplicationCommandOption {
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "username",
					Description: "The username for the account.",
					Required: true,
				},
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "password",
					Description: "The password for the account.",
					Required: true,
				},
			},
			Description: "Logs you into an account.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				options := interaction.ApplicationCommandData().Options
				username := options[0].StringValue()
				password := options[1].StringValue()
				data, err := findFromMongo(bson.M {"Username": username, "Password": password})

				webhookError(bot, err)

				if err == nil {
					if _, valid := data["UserID"]; !valid {
						bot.InteractionRespond(
							interaction.Interaction,
							&discordgo.InteractionResponse {
								Type: discordgo.InteractionResponseChannelMessageWithSource,
								Data: &discordgo.InteractionResponseData {
									Flags: 1 << 6,
									Content: "Those credentials don't match any account!",
								},
							},
						)

						return
					}

					bot.InteractionRespond(
						interaction.Interaction,
						&discordgo.InteractionResponse {
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData {
								Flags: 1 << 6,
								Content: "Logging you out of any previous account...",
							},
						},
					)

					err = updateInMongo(
						"$set", 
						bson.M {"UserID": interaction.Member.User.ID}, 
						bson.M {"UserID": ""},
					)

					webhookError(bot, err)

					if err == nil {
						bot.InteractionResponseEdit(
							bot.State.User.ID,
							interaction.Interaction,
							&discordgo.WebhookEdit { Content: "Logging into the account..." },
						)

						err = updateInMongo(
							"$set",
							bson.M {"Username": username, "Password": password},
							bson.M {"UserID": interaction.Member.User.ID},
						)

						webhookError(bot, err)

						if err == nil {
							bot.InteractionResponseEdit(
								bot.State.User.ID,
								interaction.Interaction,
								&discordgo.WebhookEdit { Content: fmt.Sprintf("You are now logged into `@%v`!", username), },
							)
						}
					}
				}
			},
		},
		"account": &customCommand {
			Group: "Personal",
			Description: "Shows info on the account you're using.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})

				webhookError(bot, err)

				if err == nil {
					blocked := 0
					contacts := 0

					for range data["ContactList"].(bson.M) {
						contacts++
					}

					for range data["BlockList"].(bson.M) {
						blocked++
					}

					bot.InteractionRespond(
						interaction.Interaction,
						&discordgo.InteractionResponse {
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData {
								Flags: 1 << 6,
								Embeds: []*discordgo.MessageEmbed {
									{
										Color: embedColor,
										Description: "This account's info.",
										Fields: []*discordgo.MessageEmbedField {
											{
												Name: "<:list:932178353010659338> Info",
												Value: strings.Join([]string {
													fmt.Sprintf("Username: `@%v`", data["Username"].(string)),
													fmt.Sprintf("Sign Up Date: `%v`", data["SignUpDate"].(string)),
													fmt.Sprintf("Emails Sent: `%v`", len(data["SentEmails"].(bson.A))),
													fmt.Sprintf("Inbox Size: `%v`", len(data["InboxedEmails"].(bson.A))),
													fmt.Sprintf("Contact List Size: `%v`", contacts),
													fmt.Sprintf("Block List Size: `%v`", blocked),
													fmt.Sprintf("Password: `%v`", data["Password"].(string)),
												}, "\n"),
												Inline: true,
											},
										},
									},
								},
							},
						},
					)
				}
			},
		},
		"addcontact": &customCommand {
			Group: "Personal",
			Description: "Adds a contact to the list.",
			Options: []*discordgo.ApplicationCommandOption {
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "username",
					Description: "The username for the contact.",
					Required: true,
				},
			},
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				username := interaction.ApplicationCommandData().Options[0].StringValue()
				data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})

				webhookError(bot, err)

				if err == nil {
					userData, err := findFromMongo(bson.M {"Username": username})

					if err == nil {
						if _, valid := userData["UserID"]; !valid {
							bot.InteractionRespond(
								interaction.Interaction,
								&discordgo.InteractionResponse {
									Type: discordgo.InteractionResponseChannelMessageWithSource,
									Data: &discordgo.InteractionResponseData {
										Flags: 1 << 6,
										Content: "That username isn't under any account!",
									},
								},
							)

							return
						}

						if _, blockedThem := (data["BlockList"].(bson.M))[username]; blockedThem {
							bot.InteractionRespond(
								interaction.Interaction,
								&discordgo.InteractionResponse {
									Type: discordgo.InteractionResponseChannelMessageWithSource,
									Data: &discordgo.InteractionResponseData {
										Flags: 1 << 6,
										Content: "You've blocked that account!",
									},
								},
							)

							return
						}

						err = updateInMongo(
							"$set", 
							bson.M {"UserID": interaction.Member.User.ID}, 
							bson.M {("ContactList." + username): true},
						)

						webhookError(bot, err)

						if err == nil {
							bot.InteractionRespond(
								interaction.Interaction,
								&discordgo.InteractionResponse {
									Type: discordgo.InteractionResponseChannelMessageWithSource,
									Data: &discordgo.InteractionResponseData {
										Flags: 1 << 6,
										Content: fmt.Sprintf("`@%v` has been added to the contact list.", username),
									},
								},
							)
						}
					}
				}
			},
		},
		"delcontact": &customCommand {
			Group: "Personal",
			Description: "Deletes a contact from the list.",
			Options: []*discordgo.ApplicationCommandOption {
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "username",
					Description: "The username for the contact.",
					Required: true,
				},
			},
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				username := interaction.ApplicationCommandData().Options[0].StringValue()
				data, err := findFromMongo(bson.M {"Username": username})

				webhookError(bot, err)

				if err == nil {
					if _, valid := data["UserID"]; !valid {
						bot.InteractionRespond(
							interaction.Interaction,
							&discordgo.InteractionResponse {
								Type: discordgo.InteractionResponseChannelMessageWithSource,
								Data: &discordgo.InteractionResponseData {
									Flags: 1 << 6,
									Content: "That username isn't under any account!",
								},
							},
						)

						return
					}

					err = updateInMongo(
						"$unset", 
						bson.M {"UserID": interaction.Member.User.ID}, 
						bson.M {("ContactList." + username): true},
					)

					webhookError(bot, err)

					if err == nil {
						bot.InteractionRespond(
							interaction.Interaction,
							&discordgo.InteractionResponse {
								Type: discordgo.InteractionResponseChannelMessageWithSource,
								Data: &discordgo.InteractionResponseData {
									Flags: 1 << 6,
									Content: fmt.Sprintf("`@%v` has been deleted from the contact list.", username),
								},
							},
						)
					}
				}
			},
		},
		"contacts": &customCommand {
			Group: "Personal",
			Description: "Lists the contacts.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})

				webhookError(bot, err)
				
				if err == nil {
					contacts := []string {}

					for name := range data["ContactList"].(bson.M) {
						contacts = append(contacts, fmt.Sprintf("`@%v`", name))
					}

					if len(contacts) <= 0 {
						contacts = append(contacts, "`...`")
					}

					bot.InteractionRespond(
						interaction.Interaction,
						&discordgo.InteractionResponse {
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData {
								Flags: 1 << 6,
								Embeds: []*discordgo.MessageEmbed {
									{
										Color: embedColor,
										Description: "Emails from contacts get inboxed normally.",
										Fields: []*discordgo.MessageEmbedField {
											{
												Name: "<:contact:932176590140473344> List",
												Value: strings.Join(contacts, ", "),
												Inline: true,
											},
										},
									},
								},
							},
						},
					)
				}
			},
		},
		"inbox": &customCommand {
			Group: "Personal",
			Description: "Lists your inboxed emails.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})

				webhookError(bot, err)

				if err == nil {
					unknown := []string {}
					normal := []string {}
					
					for _, inboxedEmail := range data["InboxedEmails"].(bson.A) {
						actualEmail := inboxedEmail.(bson.M)
						entry := fmt.Sprintf("`@%v`: %v", actualEmail["author"].(string), actualEmail["title"].(string))

						if _, valid := (data["ContactList"].(bson.M))[actualEmail["author"].(string)]; valid {
							normal = append(normal, entry)
						} else {
							unknown = append(unknown, entry)
						}
					}

					if len(unknown) <= 0 {
						unknown = append(unknown, "`...`")
					}

					if len(normal) <= 0 {
						normal = append(normal, "`...`")
					}

					bot.InteractionRespond(
						interaction.Interaction,
						&discordgo.InteractionResponse {
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData {
								Flags: 1 << 6,
								Content: "To view any email(s), use the `/search` command.",
								Embeds: []*discordgo.MessageEmbed {
									{
										Color: embedColor,
										Description: "Emails from contacts are under `Normal`.",
										Fields: []*discordgo.MessageEmbedField {
											{
												Name: "<:letter:932398954526687272> Normal",
												Value: strings.Join(normal, "\n"),
												Inline: true,
											},
											{
												Name: "<:warning:932177711307300914> Unknown",
												Value: strings.Join(unknown, "\n"),
												Inline: true,
											},
										},
									},
								},
							},
						},
					)
				}
			},
		},
		"email": &customCommand {
			Group: "Personal",
			Description: "Sends an email.",
			Options: []*discordgo.ApplicationCommandOption {
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "usernames",
					Description: "The usernames to send to (separate them with commas).",
					Required: true,
				},
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "title",
					Description: "The title for the email.",
					Required: true,
				},
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "content",
					Description: "The content for the email (the body).",
					Required: true,
				},
			},
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})

				webhookError(bot, err)
				
				if err == nil {
					options := interaction.ApplicationCommandData().Options
					usernames := strings.Split(options[0].StringValue(), ", ")
					title := options[1].StringValue()
					content := options[2].StringValue()

					bot.InteractionRespond(
						interaction.Interaction,
						&discordgo.InteractionResponse {
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData {
								Flags: 1 << 6,
								Content: "Sending emails...",
							},
						},
					)

					sent := 0

					for _, username := range usernames {
						userData, err := findFromMongo(bson.M {"Username": username})

						webhookError(bot, err)

						if _, valid := userData["UserID"]; err == nil && valid {
							_, isAContact := (userData["ContactList"].(bson.M))[data["Username"].(string)]
							_, isBlocked := (userData["BlockList"].(bson.M))[data["Username"].(string)]
							_, blockedThem := (data["BlockList"].(bson.M))[username]

							if !(userData["ProtectInbox"].(bool) && !isAContact) && !isBlocked && !blockedThem {
								entry := &email {
									Author: data["Username"].(string),
									Title: title,
									Recipients: usernames,
									Content: strings.ReplaceAll(content, "\\n", "\n"),
									Date: createDate(time.Now()),
								}

								err = updateInMongo(
									"$push",
									bson.M {"Username": username},
									bson.M {"InboxedEmails": entry},
								)

								webhookError(bot, err)

								sent++

								if err == nil {
									err = updateInMongo(
										"$push",
										bson.M {"UserID": interaction.Member.User.ID},
										bson.M {"SentEmails": entry},
									)

									webhookError(bot, err)
								}
							}
						}
					}

					bot.InteractionResponseEdit(
						bot.State.User.ID,
						interaction.Interaction,
						&discordgo.WebhookEdit { Content: fmt.Sprintf("`%v` emails were sent, nice!", sent) },
					)
				}
			},
		},
		"search": &customCommand {
			Group: "Personal",
			Description: "Shows similar emails from a search.",
			Options: []*discordgo.ApplicationCommandOption {
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "type",
					Description: "The type of email to search for (inboxed or sent).",
					Required: true,
				},
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "body",
					Description: "The title/content to search through any emails for.",
					Required: true,
				},
			},
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})

				webhookError(bot, err)

				if err == nil {
					bot.InteractionRespond(
						interaction.Interaction,
						&discordgo.InteractionResponse {
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData {
								Flags: 1 << 6,
								Content: "Searching through all emails...",
							},
						},
					)

					similar := 0
					files := []*discordgo.File {}
					options := interaction.ApplicationCommandData().Options
					body := options[1].StringValue()
					emailType := "InboxedEmails"

					if options[0].StringValue() == "sent" {
						emailType = "SentEmails"
					}

					for _, inboxedEmail := range data[emailType].(bson.A) {
						actualEmail := inboxedEmail.(bson.M)
						
						if compare(body, actualEmail["title"].(string)) >= 0.4 || compare(body, actualEmail["content"].(string)) >= 0.4 {
							recipients := []string {}

							for _, recipient := range actualEmail["recipients"].(bson.A) {
								recipients = append(recipients, "@" + recipient.(string))
							}

							files = append(files, &discordgo.File {
								Name: actualEmail["title"].(string) + ".txt",
								Reader: bytes.NewReader([]byte(fmt.Sprintf(
									"Title: \"%v\"\nAuthor: @%v\nDate: %v\nRecipients: %v\nContent:\n\n%v",
									actualEmail["title"].(string),
									actualEmail["author"].(string),
									actualEmail["date"].(string),
									strings.Join(recipients, ", "),
									actualEmail["content"].(string),
								))),
							})

							similar++

							if similar >= 10 {
								break
							}
						}
					}

					bot.InteractionResponseEdit(
						bot.State.User.ID,
						interaction.Interaction,
						&discordgo.WebhookEdit {
							Content: fmt.Sprintf("`%v` emails were searched and pulled.", similar),
							Files: files,
						},
					)
				}
			},
		},
		"commands": &customCommand {
			Group: "Fun",
			Description: "Shows the list of commands.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				commands := []string {}

				for name := range listAppCommands() {
					commands = append(commands, fmt.Sprintf("`/%v`", name))
				}

				bot.InteractionRespond(
					interaction.Interaction,
					&discordgo.InteractionResponse {
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionResponseData {
							Flags: 1 << 1,
							Embeds: []*discordgo.MessageEmbed {
								{
									Color: embedColor,
									Fields: []*discordgo.MessageEmbedField {
										{
											Name: "<:list:932178353010659338> Slash Commands",
											Value: strings.Join(commands, ", "),
											Inline: true,
										},
									},
								},
							},
						},
					},
				)
			},
		},
		"protection": &customCommand {
			Group: "Personal",
			Description: "Turns inbox protection on or off.",
			Options: []*discordgo.ApplicationCommandOption {
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "status",
					Description: "On or off.",
					Required: true,
				},
			},
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				err := errors.New("")
				status := interaction.ApplicationCommandData().Options[0].StringValue()

				switch status {
				case "off":
					err = updateInMongo(
						"$set",
						bson.M {"UserID": interaction.Member.User.ID},
						bson.M {"ProtectInbox": false},
					)
				default:
					err = updateInMongo(
						"$set",
						bson.M {"UserID": interaction.Member.User.ID},
						bson.M {"ProtectInbox": true},
					)
				}

				webhookError(bot, err)

				if err == nil {
					bot.InteractionRespond(
						interaction.Interaction,
						&discordgo.InteractionResponse {
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData {
								Flags: 1 << 6,
								Content: fmt.Sprintf("Inbox protection toggled `%v`.", status),
							},
						},
					)
				}
			},
		},
		"settings": &customCommand {
			Group: "Personal",
			Description: "Shows all settings.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})

				webhookError(bot, err)

				if err == nil {
					twoFA := data["2FA"].(bson.M)
					question := twoFA["question"].(string)
					answer := twoFA["answer"].(string)

					if question == "" {
						question = " "
					}

					if answer == "" {
						answer = " "
					}

					bot.InteractionRespond(
						interaction.Interaction,
						&discordgo.InteractionResponse {
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData {
								Flags: 1 << 6,
								Embeds: []*discordgo.MessageEmbed {
									{
										Color: embedColor,
										Description: "These are the current account settings.",
										Fields: []*discordgo.MessageEmbedField {
											{
												Name: "<:gear:932392637925822556> Settings",
												Value: strings.Join([]string {
													fmt.Sprintf("Inbox Protection: `%v`", data["ProtectInbox"].(bool)),
													fmt.Sprintf(
														"2FA: `%v`\n<:blank:932849399598551082>**>** Question: `%v`\n<:blank:932849399598551082>**>** Answer: `%v`",
														twoFA["active"].(bool),
														question,
														answer,
													),
												}, "\n"),
												Inline: true,
											},
										},
									},
								},
							},
						},
					)
				}
			},
		},
		"docs": &customCommand {
			Group: "Fun",
			Description: "Shows the docs/guide.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				bot.InteractionRespond(
					interaction.Interaction,
					&discordgo.InteractionResponse {
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionResponseData {
							Flags: 1 << 1,
							Embeds: []*discordgo.MessageEmbed {
								{
									Color: embedColor,
									Description: "These are the docs for my services.",
									Fields: []*discordgo.MessageEmbedField {
										{
											Name: "<:contact:932176590140473344> Contacts",
											Value: "Emails from contacts get sorted under the `Normal` inbox category. Contacts are like friends, and can be removed and added as you please. There might be more potential for contacts in the future, but the only perk that comes with being a contact is being recognized as someone with more access than a normal account.",
											Inline: true,
										},
										{
											Name: "<:no:932418336229326878> Blocking",
											Value: "Blocking an account means they can't send you any emails whatsoever, and it will automatically remove them from the contacts list. This actually saves database space, so it is suggested to block those you don't want contacting you through my services.",
											Inline: true,
										},
										{
											Name: "<:letter:932398954526687272> Emails",
											Value: "An email contains the recipients, author, date, title, and content. To put new lines in an email, use \"`\\n`\". When searching emails, they are returned in a list of files. The file contains all necessary info, and is titled what the email was titled. This makes it easy to store larger emails and makes them downloadable. When searching through emails, any title or content that is **40%** similar to the search body will be pulled.",
											Inline: true,
										},
										{
											Name: "<:list:932178353010659338> Slash Commands",
											Value: "Some command responses will only be visible to the account who runs said command(s). Everything is done through slash commands, and message commands will never be available.",
											Inline: true,
										},
										{
											Name: "<:gear:932392637925822556> Settings",
											Value: "`Inbox protection` prevents any emails **NOT** from contacts from sending, and saves database space for those who don't want to be emailed by strangers. `2FA` puts an additional question for the user logging in to get access, making it more secure for the creator of the account. `2FA` has yet to arrive.",
											Inline: true,
										},
										{
											Name: "<:no:932418336229326878> Errors",
											Value: "If the bot doesn't respond or stops working, that means an error has appeared. Usually, if the bot continues to work, it is sent to my developers and handled.",
											Inline: true,
										},
									},
								},
							},
						},
					},
				)
			},
		},
		"block": &customCommand {
			Group: "Personal",
			Description: "Blocks an account.",
			Options: []*discordgo.ApplicationCommandOption {
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "username",
					Description: "The username to block.",
					Required: true,
				},
			},
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				username := interaction.ApplicationCommandData().Options[0].StringValue()
				data, err := findFromMongo(bson.M {"Username": username})

				webhookError(bot, err)

				if err == nil {
					if _, valid := data["UserID"]; !valid {
						bot.InteractionRespond(
							interaction.Interaction,
							&discordgo.InteractionResponse {
								Type: discordgo.InteractionResponseChannelMessageWithSource,
								Data: &discordgo.InteractionResponseData {
									Flags: 1 << 6,
									Content: "That username isn't under any account!",
								},
							},
						)

						return
					}

					err = updateInMongo(
						"$set", 
						bson.M {"UserID": interaction.Member.User.ID}, 
						bson.M {("BlockList." + username): true},
					)

					webhookError(bot, err)

					if err == nil {
						err = updateInMongo(
							"$unset", 
							bson.M {"UserID": interaction.Member.User.ID}, 
							bson.M {("ContactList." + username): true},
						)

						webhookError(bot, err)

						if err == nil {
							bot.InteractionRespond(
								interaction.Interaction,
								&discordgo.InteractionResponse {
									Type: discordgo.InteractionResponseChannelMessageWithSource,
									Data: &discordgo.InteractionResponseData {
										Flags: 1 << 6,
										Content: fmt.Sprintf("`@%v` has been blocked.", username),
									},
								},
							)
						}
					}
				}
			},
		},
		"unblock": &customCommand {
			Group: "Personal",
			Description: "Unblocks an account.",
			Options: []*discordgo.ApplicationCommandOption {
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "username",
					Description: "The username to unblock.",
					Required: true,
				},
			},
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				username := interaction.ApplicationCommandData().Options[0].StringValue()
				data, err := findFromMongo(bson.M {"Username": username})

				webhookError(bot, err)

				if err == nil {
					if _, valid := data["UserID"]; !valid {
						bot.InteractionRespond(
							interaction.Interaction,
							&discordgo.InteractionResponse {
								Type: discordgo.InteractionResponseChannelMessageWithSource,
								Data: &discordgo.InteractionResponseData {
									Flags: 1 << 6,
									Content: "That username isn't under any account!",
								},
							},
						)

						return
					}

					err = updateInMongo(
						"$unset", 
						bson.M {"UserID": interaction.Member.User.ID}, 
						bson.M {("BlockList." + username): true},
					)

					webhookError(bot, err)

					if err == nil {
						bot.InteractionRespond(
							interaction.Interaction,
							&discordgo.InteractionResponse {
								Type: discordgo.InteractionResponseChannelMessageWithSource,
								Data: &discordgo.InteractionResponseData {
									Flags: 1 << 6,
									Content: fmt.Sprintf("`@%v` has been unblocked.", username),
								},
							},
						)
					}
				}
			},
		},
		"blocked": &customCommand {
			Group: "Personal",
			Description: "Lists the blocked accounts.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})

				webhookError(bot, err)
				
				if err == nil {
					blocked := []string {}

					for name := range data["BlockList"].(bson.M) {
						blocked = append(blocked, fmt.Sprintf("`@%v`", name))
					}

					if len(blocked) <= 0 {
						blocked = append(blocked, "`...`")
					}

					bot.InteractionRespond(
						interaction.Interaction,
						&discordgo.InteractionResponse {
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData {
								Flags: 1 << 6,
								Embeds: []*discordgo.MessageEmbed {
									{
										Color: embedColor,
										Description: "Emails from blocked accounts will not be received.",
										Fields: []*discordgo.MessageEmbedField {
											{
												Name: "<:no:932418336229326878> Blocked",
												Value: strings.Join(blocked, ", "),
												Inline: true,
											},
										},
									},
								},
							},
						},
					)
				}
			},
		},
		"botinfo": &customCommand {
			Group: "Fun",
			Description: "Shows info on me.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				memory := runtime.MemStats {}

				runtime.ReadMemStats(&memory)

				bar, percent := showProgress(int(memory.Alloc), int(memory.Sys), 15)

				bot.InteractionRespond(
					interaction.Interaction,
					&discordgo.InteractionResponse {
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionResponseData {
							Flags: 1 << 1,
							Embeds: []*discordgo.MessageEmbed {
								{
									Color: embedColor,
									Fields: []*discordgo.MessageEmbedField {
										{ 
											Name: "<:gear:932392637925822556> Info",
											Value: strings.Join([]string {
												"Library: [**discordgo**](https://github.com/bwmarrin/discordgo)",
												fmt.Sprintf("Go Version: `%v`", runtime.Version()),
												"Support: [**Etsuko Support**](https://discord.gg/n76N8NVksD)",
												"Invite: [**OAuth2**](https://discord.com/api/oauth2/authorize?client_id=931702913523388436&permissions=139586817088&scope=bot%20applications.commands)",
												fmt.Sprintf("Guilds: `%v`", guildCount),
												fmt.Sprintf("Users: `%v`", userCount),
												fmt.Sprintf("Memory Usage:\n`%v` **%v%v**", bar, percent, "%"),
											}, "\n"),
											Inline: true,
										},
									},
								},
							},
						},
					},
				)
			},
		},
		"delete": &customCommand {
			Group: "Personal",
			Description: "Deletes an email of a type.",
			Options: []*discordgo.ApplicationCommandOption {
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "type",
					Description: "The type of email to delete (inboxed or sent).",
					Required: true,
				},
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "title",
					Description: "The title of the email to delete.",
					Required: true,
				},
			},
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})
				
				if err == nil {
					emails := []*email {}
					options := interaction.ApplicationCommandData().Options	
					emailType := "InboxedEmails"
					
					if options[0].StringValue() == "sen" {
						emailType = "SentEmails"
					}

					for _, inboxedEmail := range data[emailType].(bson.A) {
						actualEmail := inboxedEmail.(bson.M)

						if actualEmail["title"].(string) != options[1].StringValue() {
							recipients := []string {}

							for _, recipient := range actualEmail["recipients"].(bson.A) {
								recipients = append(recipients, recipient.(string))
							}

							emails = append(emails, &email {
								Author: actualEmail["author"].(string),
								Title: actualEmail["title"].(string),
								Recipients: recipients,
								Content: actualEmail["content"].(string),
								Date: actualEmail["date"].(string),
							})
						}
					}

					err := updateInMongo(
						"$set",
						bson.M {"UserID": interaction.Member.User.ID},
						bson.M {emailType: emails},
					)

					webhookError(bot, err)

					if err == nil {
						bot.InteractionRespond(
							interaction.Interaction,
							&discordgo.InteractionResponse {
								Type: discordgo.InteractionResponseChannelMessageWithSource,
								Data: &discordgo.InteractionResponseData {
									Flags: 1 << 6,
									Content: "The email has been deleted.",
								},
							},
						)
					}
				}
			},
		},
		"deleteall": &customCommand {
			Group: "Personal",
			Description: "Deletes all emails of a type.",
			Options: []*discordgo.ApplicationCommandOption {
				{
					Type: discordgo.ApplicationCommandOptionString,
					Name: "type",
					Description: "The type of emails to delete (inboxed or sent).",
					Required: true,
				},
			},
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				err := errors.New("")

				switch interaction.ApplicationCommandData().Options[0].StringValue() {
				case "sent":
					err = updateInMongo(
						"$set",
						bson.M {"UserID": interaction.Member.User.ID},
						bson.M {"SentEmails": []*email {}},
					)
				default:
					err = updateInMongo(
						"$set",
						bson.M {"UserID": interaction.Member.User.ID},
						bson.M {"InboxedEmails": []*email {}},
					)
				}

				if err == nil {
					bot.InteractionRespond(
						interaction.Interaction,
						&discordgo.InteractionResponse {
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData {
								Flags: 1 << 6,
								Content: "All emails of that type have been deleted.",
							},
						},
					)
				}
			},
		},
		"sent": &customCommand {
			Group: "Personal",
			Description: "Shows all emails sent on this account.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				data, err := findFromMongo(bson.M {"UserID": interaction.Member.User.ID})

				webhookError(bot, err)

				if err == nil {
					emails := []string {}
					
					for _, sentEmail := range data["SentEmails"].(bson.A) {
						actualEmail := sentEmail.(bson.M)
						emails = append(emails, fmt.Sprintf("`@%v`: %v", actualEmail["author"].(string), actualEmail["title"].(string)))
					}

					if len(emails) <= 0 {
						emails = append(emails, "`...`")
					}

					bot.InteractionRespond(
						interaction.Interaction,
						&discordgo.InteractionResponse {
							Type: discordgo.InteractionResponseChannelMessageWithSource,
							Data: &discordgo.InteractionResponseData {
								Flags: 1 << 6,
								Content: "To view any email(s), use the `/search` command.",
								Embeds: []*discordgo.MessageEmbed {
									{
										Color: embedColor,
										Description: "These are all emails sent from this account.",
										Fields: []*discordgo.MessageEmbedField {
											{
												Name: "<:letter:932398954526687272> Emails",
												Value: strings.Join(emails, "\n"),
												Inline: true,
											},
										},
									},
								},
							},
						},
					)
				}
			},
		},
		"policy": &customCommand {
			Group: "Fun",
			Description: "Shows the privacy policy.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				bot.InteractionRespond(
					interaction.Interaction,
					&discordgo.InteractionResponse {
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionResponseData {
							Flags: 1 << 1,
							Embeds: []*discordgo.MessageEmbed {
								{
									Color: embedColor,
									Description: "The privacy policy describes Etsuko's DOs and DON'Ts.",
									Fields: []*discordgo.MessageEmbedField {
										{
											Name: "<:gear:932392637925822556> Policy",
											Value: "The only data stored by Etsuko is `custom data`, as well as your `user ID`. We do not store your actual Discord account's password, or anything related to it. Etusko acts as its own service when it comes to accounts, meaning your Etsuko account is only related to Etsuko, nothing else. Furthermore, if you wish to have your data removed from the database, you may contact one of the developers of Etsuko.",
											Inline: true,
										},
									},
								},
							},
						},
					},
				)
			},
		},
		"terms": &customCommand {
			Group: "Fun",
			Description: "Shows the term(s) of service.",
			Run: func(bot *discordgo.Session, interaction *discordgo.InteractionCreate) {
				bot.InteractionRespond(
					interaction.Interaction,
					&discordgo.InteractionResponse {
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionResponseData {
							Flags: 1 << 1,
							Embeds: []*discordgo.MessageEmbed {
								{
									Color: embedColor,
									Description: "Your account can be deleted whenever, for any reason. that is the only term.",
								},
							},
						},
					},
				)
			},
		},
	}
}

func formatMonth(month time.Month) string {
	switch month {
	case 1: return "January"
	case 2: return "February"
	case 3: return "March"
	case 4: return "April"
	case 5: return "May"
	case 6: return "June"
	case 7: return "July"
	case 8: return "August"
	case 9: return "September"
	case 10: return "October"
	case 11: return "November"
	case 12: return "December"
	}

	return "January"
}

func formatNumberPos(number int) string {
	toString := strconv.Itoa(number)
	split := strings.Split(toString, "") 
	endNumber, err := strconv.Atoi(split[len(split) - 1])

	if err != nil || endNumber == 1 {
		return toString + "st"
	} else if endNumber == 2 {
		return toString + "nd"
	} else if endNumber == 3 {
		return toString + "rd"
	} else if endNumber > 3 {
		return toString + "th"
	}

	return toString + "st"
}

func createDate(now time.Time) string {
	return fmt.Sprintf(
		"%v %v, %v",
		formatMonth(now.Month()),
		formatNumberPos(now.Day()),
		now.Year(),
	)
}

func webhookError(bot *discordgo.Session, err error) {
	if err != nil {
		bot.WebhookExecute(
			"932489231958433893",
			"nwW2afhvBAznwb9m4erwmsiFf-oWrEUdaK6Ser3bZbaevFqeD0eI1CzjiRTEprXDciNq",
			false,
			&discordgo.WebhookParams {
				Embeds: []*discordgo.MessageEmbed {
					{
						Color: embedColor,
						Description: "An error has appeared!",
						Fields: []*discordgo.MessageEmbedField {
							{
								Name: "<:no:932418336229326878> Error",
								Value: fmt.Sprintf("`%v`", err),
								Inline: true,
							},
						},
					},
				},
			},
		)
	}
}

func compare(first string, second string) float32 {
	cleanse(&first)
	cleanse(&second)

	firstBigrams := map[string]int {}

	for i := 0; i < len(first) - 1; i++ {
		a := fmt.Sprintf("%c", first[i])
		b := fmt.Sprintf("%c", first[i + 1])

		bigram := a + b

		var count int

		if value, ok := firstBigrams[bigram]; ok {
			count = value + 1
		} else {
			count = 1
		}

		firstBigrams[bigram] = count
	}

	var intersectionSize float32
	intersectionSize = 0

	for i := 0; i < len(second) - 1; i++ {
		a := fmt.Sprintf("%c", second[i])
		b := fmt.Sprintf("%c", second[i + 1])

		bigram := a + b

		var count int

		if value, ok := firstBigrams[bigram]; ok {
			count = value
		} else {
			count = 0
		}

		if count > 0 {
			firstBigrams[bigram] = count - 1
			intersectionSize = intersectionSize + 1
		}
	}

	return (2.0 * intersectionSize) / (float32(len(first)) + float32(len(second)) - 2)
}

func cleanse(body *string) {
	*body = strings.ReplaceAll(*body, " ", "")
}

func showProgress(start, max, size int) (string, float64) {
	percent := float64(start) / float64(max)
	progress := math.Round(float64(float64(size) * percent))
	container := float64(size) - progress

	return fmt.Sprintf(
		"[%v%v]",
		strings.Repeat("▇", int(progress)),
		strings.Repeat(" ឵឵", int(container)),
	), math.Round(float64(percent * 100))
}