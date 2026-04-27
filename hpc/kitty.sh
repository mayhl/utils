#!/usr/bin/env zsh

SSH_HPC_URLS=($(set | grep --line-buffered -i "_SSH=mayhl" | cut -d " " -f2 | cut -d "=" -f2 | xargs))

for url in ${SSH_HPC_URLS[@]}; do
  name=$(echo $url | cut -d . -f1 | cut -d @ -f2)
  echo "Updating: $name"
  timeout 10 kitty +kitten ssh $url "exit"
  if [ $? ]; then
    echo "   FAILED"
  else
    echo "   SUCCESS"
  fi
done

unset SSH_HPC_URLS
