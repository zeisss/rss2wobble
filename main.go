// Tool to fetch RSS feeds and sync them with the Wobble API.
package main

import (
  wobble "github.com/ZeissS/wobble-go-client"
  rss "github.com/ungerik/go-rss"
  flag "github.com/ogier/pflag"

  "crypto/md5"
  "encoding/json"
  "fmt"
  "io"
  "log"
  "os"
  "text/template"
  "time"
)

// The main configuration file.
type Configuration struct {
  WobbleConfig WobbleConfig `json:"wobble"`
  Feeds []Feed `json:"feeds"`
}
type WobbleConfig struct {
  Endpoint string `json:"endpoint"`
  Username string `json:"username"`
  Password string `json:"password"`
}
// The RSS feeds to sync.
type Feed struct {
  Name *string `json:"name"`
  Url string `json:"url"`
  MaxItems *uint `json:"max-items"`
}

var err error
var config *Configuration

func main() {
  config_file := flag.StringP("config", "c", "config.json", "The configuration file.")
  flag.Parse()

  config, err = GetConfiguration(*config_file)
  if err != nil {
    log.Fatalf("Failed to read %v: %v", *config_file, err)
  }

  client := wobble.NewClient(config.WobbleConfig.Endpoint)

  err = client.Login(config.WobbleConfig.Username, config.WobbleConfig.Password)
  if err != nil {
    log.Fatalf("Failed to authenticate with service: %v", err)
  }
  defer client.Logout()

  for _, feed := range config.Feeds {
    log.Printf("Syncing feed %v...\n", feed.Url)
    SyncFeed(config.WobbleConfig.Username, client, feed)
  }
}

// Load the configuration file.
func GetConfiguration(filename string) (*Configuration, error) {
  var config Configuration
  var err error

  fp, err := os.Open(filename)
  if err != nil {
    return nil, err
  }

  var data []byte = make([]byte, 1024 * 1024 * 1024) // 1mb
  n, err := fp.Read(data)
  if err != nil {
    return nil, err
  }
  if n == 1024 * 1024 * 1024 {
    return nil, fmt.Errorf("Configuration file is huge! Aborting parsing!")
  }

  err = json.Unmarshal(data[0:n], &config)
  if err != nil {
    return nil, err
  }
  return &config, nil
}

// This syncs the content of the RSS feed with corresponding wobble-topic.
//
// The topic is identified by its ID which is a hashed combination of the
// user-id and the feed-url. Thus this should be unique.
//
// Inside each topic each post corresponds to a feed-item. The SyncFeed()
// method will create new posts for new entries, update existing ones and
// delete removed one.
//
// The root post of each topic (id=1) works as a generic info post.
func SyncFeed(username string, client *wobble.Client, feed Feed) {
  // Get the topic
  var topic_id string = Hash(feed.Url, username)
  topic, err := client.GetTopic(topic_id)
  if err != nil {
    // Ok, blind idiotism. If we failed to get the topic, it may just don't exists. So lets try again creating it!
    err = client.CreateTopic(topic_id)
    if err != nil {
      log.Fatalf("Failed to create topic: %v", err)
    }

    topic, err = client.GetTopic(topic_id)
  }

  // Get the channel
  channel, err := rss.Read(feed.Url)
  if err != nil {
    log.Printf("Failed to fetch %v: %v", feed.Url, err)
    return
  }

  // Shorten channel items
  if feed.MaxItems != nil && uint(len(channel.Item)) > *feed.MaxItems {
    channel.Item = channel.Item[0:*feed.MaxItems]
  }

  // Update root post
  rootContent := ComposeRootContent(feed, channel)
  if topic.Posts[0].PostId == "1" && topic.Posts[0].Content != nil && *topic.Posts[0].Content != rootContent {
    _, err = client.EditPost(topic_id, "1", rootContent, topic.Posts[0].RevisionNo)
    if err != nil {
      log.Printf("Failed to edit root post. t: %v p: %v - %v", topic_id, "1", err)
    }
  }
  
  // Now compare
  outdated_posts := FilterOutdatedPosts(topic, channel)
  for _, post_id := range outdated_posts {
    time.Sleep(1 * time.Second)
    _, err := client.DeletePost(topic_id, post_id)
    if err != nil {
      log.Printf("Failed to delete post. t: %v p: %v - %v", topic_id, post_id, err)
    }
  }

  for _, channel_item := range FilterNewItems(topic, channel) {
    time.Sleep(1 * time.Second)
    log.Printf("Creating new post for channel item %v\n", channel_item.GUID)
    item_post_id := Hash(channel.Link, channel_item.GUID)

    _, err := client.CreatePost(topic_id, item_post_id, "1", true)
    if err != nil {
      log.Printf("Failed to create post. t: %v p: %v - %v", topic_id, item_post_id, err)
      continue
    }

    _, err = client.EditPost(topic_id, item_post_id, ComposePostContent(&channel_item), 1)
    if err != nil {
      log.Printf("Failed to edit post. t: %v p: %v - %v", topic_id, item_post_id, err)
      continue
    }

    err = client.ChangePostRead(topic_id, item_post_id, false)
    if err != nil {
      log.Printf("Failed to mark post unread. t: %v p: %v - %v", topic_id, item_post_id, err)
      continue
    }
  }

  for _, item := range channel.Item {
    item_post_id := Hash(channel.Link, item.GUID)
    content := ComposePostContent(&item)

    for _, post := range topic.Posts {
      if post.PostId == item_post_id {
        if post.Content != nil && *post.Content != content {
          time.Sleep(1 * time.Second)

          _, err = client.EditPost(topic_id, item_post_id, content, post.RevisionNo)
          if err != nil {
            log.Printf("Failed to edit post. t: %v p: %v - %v", topic_id, item_post_id, err)
          }

          err = client.ChangePostRead(topic_id, item_post_id, false)
          if err != nil {
            log.Printf("Failed to mark post unread. t: %v p: %v - %v", topic_id, item_post_id, err)
            continue
          }
        }
        
      }
    }
  }
}

func ComposeRootContent(feed Feed, channel *rss.Channel) string {
  var title string = channel.Title
  if feed.Name != nil {
    title = *feed.Name
  }
  return "<div>[FEED] <b>" + title + "</b></div><br><br>" +
         "<p>" + channel.Description + "</p>" + 
         "<a href=\"" + channel.Link + "\">Homepage</a>"
}

func ComposePostContent(item *rss.Item) string {
  var content string = "<div>" + template.HTMLEscapeString(item.Title) + "</div>" +
         "<p>" + 
         "<b>Datum:</b> " + string(item.PubDate) + "<br>" + 
         "<b>URL:</b> <a href=\"" + item.Link + "\">" + Shorten(item.Link, 100) + "...</a>" + 
         "</p>";

  if len(item.Content) > 0 {
    content = content + "<br /><p>" + item.Content + "</p>"
  } else {
    content = content + "<br /><p>" + item.Description + "</p>"
  }
  return content
}

func FilterExistingItems(topic *wobble.Topic, channel *rss.Channel) []rss.Item {
  items := make([]rss.Item, 0)
  for _, item := range channel.Item {
    found := false
    item_post_id := Hash(channel.Link, item.GUID)

    for _, post := range topic.Posts {
      if post.PostId == item_post_id {
        found = true
      }
    }

    if found {
      items = append(items, item)
    }
  }
  
  return items
}

func FilterNewItems(topic *wobble.Topic, channel *rss.Channel) []rss.Item {
  items := make([]rss.Item, 0)
  for _, item := range channel.Item {
    found := false
    item_post_id := Hash(channel.Link, item.GUID)

    for _, post := range topic.Posts {
      if post.PostId == item_post_id {
        found = true
      }
    }

    if !found {
      items = append(items, item)
    }
  }
  
  return items
}

func FilterOutdatedPosts(topic *wobble.Topic, channel *rss.Channel) []string {
  outdated := make([]string, 0)

  for _, post := range topic.Posts {
    if post.PostId == "1" || // We always keep the root post
       post.Deleted == 1 || // No need to re-delete 
       post.Unread == 1 {  // Skip if the user hasn't read it yet
      continue
    }
    var found bool = false
    for _, item := range channel.Item {
      item_post_id := Hash(channel.Link, item.GUID)
      if post.PostId == item_post_id {
        found = true
      }
    }

    if !found {
      outdated = append(outdated, post.PostId)
    }
  }

  return outdated
}

// Runs a simple md5 hashing over the passed combined keys
func Hash(keys ...string) string {
  h := md5.New()

  for index, k := range(keys) {
    if index != 0 {
      io.WriteString(h, ":") // Delimiter
    }
    io.WriteString(h, k)
  }
  return fmt.Sprintf("%x", h.Sum(nil))
}

// Shortens the given string to maximally the number of given characters.
// If shortened, the string "..." is appended.
func Shorten (text string, max_length uint) string {
  if uint(len(text)) > max_length {
    return text[0:max_length] + "..."
  }
  return text
}