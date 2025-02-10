#!/bin/sh

umask 0002

INIT_DIR=$(dirname $0)/init

if [[ -f ./init/00-style.sh ]]; then
  source ./init/00-style.sh
fi

for file in `ls -1 $INIT_DIR/ | sort`; do
    file=$INIT_DIR/$file

    if [ -x $file ]; then
        sh $file
    fi
done

supervisord -c /etc/supervisord.conf
