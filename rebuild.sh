if [ "$1" == "nocontainer" ]; then
  echo "Starting new container.."
else
  echo "Restarting container..!"
  docker stop discord_listener && docker rm discord_listener
fi
docker build -t discord_listener . && docker run --network br0 -d --name discord_listener --env-file .env discord_listener
