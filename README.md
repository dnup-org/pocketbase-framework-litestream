# PocketBase & Litestream Example

This repo is a starting spot to use PocketBase as a framework with Go that is backed up and restored via Litestream.

## Why does this exist?

PocketBase is an amazing project that can accomplish 99% of what a side-hustle, POC, and even startup needs. However, when using as a framework, we need to be able update code without needing to manually back up the DB file. I wanted to deploy my code in a container to [Fly.io](https://fly.io) but still utilize the magic of Litestream. This repo allows you to copy/paste the starting files to do just that.

While it isn't strictly necessary, I've also added volume to the fly machine.

## Usage

### Prerequisites

You'll need to have an S3-compatible store to connect to. Please see the [Litestream Guides](https://litestream.io/guides/) to get set up on your preferred object store.

## Local Development

Since this is using PocketBase as a Go framework, you can run this locally with `go run main.go serve --http "localhost:8080"`

You will want to do this to not use your Litestream backed up DB in development.

## Deploying to Fly.io

First, ensure that you have a `.auth.env` file with the following variables:
```
LITESTREAM_ACCESS_KEY_ID=xxxxxxxxxxxxxxxxxxxx
LITESTREAM_SECRET_ACCESS_KEY=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
REPLICA_URL="s3://YOUR_S3_BUCKET_NAME/db"
```

Next, publish your environment file to Fly.io.
```bash
cat .auth.env | fly secrets import
```

Finally, deploy to Fly.io
```
fly deploy --local-only
```

## Restoring from a backup
First, make sure you have litestream installed locally.
Next, reconstruct the replica and poke around with sqlite3.
```
./do 'litestream restore -o db.sqlite $REPLICA_URL'

# Poke around
sqlite3 db.sqlite
```
