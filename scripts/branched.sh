#!/bin/bash
set -e

echo "Branching from Production"
# Restore the database if it does not already exist.
# Use the directory our binary is in `/usr/local/bin/` to properly replicate
# and restore with PocketBase's default locaiton for the database.
if [ -f /usr/local/bin/pb_data/data.db ]; then
  echo "Database already exists, skipping restore"
else
  echo "No database found, restoring from replica at ${REPLICA_URL}"
  litestream restore -v -if-replica-exists -o /usr/local/bin/pb_data/data.db /usr/local/bin/pb_data/data.db
fi

exec /usr/local/bin/app --hooksDir=/usr/local/bin/pb_hooks --dev serve_h2c --http 0.0.0.0:8080
