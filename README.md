# rss2wobble

A small utility to publish RSS feeds as topics on your wobble server.

## Configuration
Modify the `config.json` file to match your needs.

```
{
  "wobble": {
    "username": "YourUsername",
    "password": "YourPassword",
    "endpoint": "EndpointUrl"
  },
  "feeds": [
      Feed
  ]
}
```

Each `Feed` is an object with one to three fields:

 * `url` This is required and describes the URL to the RSS feed
 * `max-items` can be set, if only the first `max-items` should be read from the feed. The rest is discarded.
 * `name` Overwrites the name of the feed when generating the root post.

## Usage

```
rss2wobble [-c config.json]
```

 *  `-c --config <file>` Loads a different configuration than the default: `./config.json`

