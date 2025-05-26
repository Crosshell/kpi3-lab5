#!/bin/sh

case "$1" in
    "db")
        exec /opt/practice-4/db
        ;;
    "server")
        exec /opt/practice-4/server
        ;;
    "lb")
        exec /opt/practice-4/balancer
        ;;
    *)
        echo "Unknown command: $1"
        exit 1
        ;;
esac