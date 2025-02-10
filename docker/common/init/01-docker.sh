#!/bin/sh

# Check if we need to configure the container timezone
if [ ! -z "$TZ" ]; then
	TZ_FILE="/usr/share/zoneinfo/$TZ"
	if [ -f "$TZ_FILE" ]; then
		echo  -e "‣ $NOTICE Setting container timezone to: ${EMPHASIS}$TZ${RESET}"
		ln -snf "$TZ_FILE" /etc/localtime 
		echo "$TZ" > /etc/timezone 
	else
		echo  -e "‣ $WARN Cannot set timezone to: ${EMPHASIS}$TZ${RESET} -- this timezone does not exist."
	fi
else
	echo  -e "‣ $INFO Not setting any timezone for the container"
fi
