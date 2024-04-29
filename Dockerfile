FROM golang:1.22.2

WORKDIR /usr/src/app

# Copy go files
COPY go.mod go.sum main.go ./

# Build the binary
COPY handler /usr/src/app/handler
RUN go mod download && go mod verify
RUN go build -v -o /usr/local/bin/app

# Copy public files
COPY pb_public /usr/local/bin/pb_public

# Set ENVs
# These should be set via an env files
# Locally you can run docker with --env-file
# On Fly you should set these by piping your env file to `fly secrets import`
ENV LITESTREAM_ACCESS_KEY_ID=xxxxxxxxxxxxxxxxxxxx
ENV LITESTREAM_SECRET_ACCESS_KEY=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
ENV REPLICA_URL="s3://YOUR_S3_BUCKET_NAME/db"

# Download the static build of Litestream directly into the path & make it executable.
# This is done in the builder and copied as the chmod doubles the size.
ADD https://github.com/benbjohnson/litestream/releases/download/v0.3.9/litestream-v0.3.9-linux-amd64-static.tar.gz /tmp/litestream.tar.gz
RUN tar -C /usr/local/bin -xzf /tmp/litestream.tar.gz

# Notify Docker that the container wants to expose a port.
# Pocketbase serve port
# Use port 8080 for deploying to Fly.io, GCP Cloud Run, or AWS App Runner easily.
EXPOSE 8080 
# For the litestream server via Prometheus if using https://litestream.io/reference/config/#metrics
EXPOSE 9090 

# Copy Litestream configuration file & startup script.
COPY etc/litestream.yml /etc/litestream.yml
COPY scripts/run.sh /scripts/run.sh

RUN chmod +x /scripts/run.sh
RUN chmod +x /usr/local/bin/litestream


# Start Pocketbase
CMD [ "/scripts/run.sh" ]
