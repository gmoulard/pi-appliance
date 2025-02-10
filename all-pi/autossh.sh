#!/bin/bash
#
# description: setup autossh for gm pi cluster
# V0 : 07/04/21 By guillaume@moulard.org - creation
set -x

#  cat /sys/firmware/devicetree/base/model
# https://www.raspberrypi.org/documentation/hardware/raspberrypi/revision-codes/README.md
# sudo crontab -e
# @reboot /home/pi/pi-appliance/autossh.sh >> /var/log/pi-appliance.log 2>> /var/log/pi-appliance.err
# curl https://raw.githubusercontent.com/gmoulard/pi-appliance/master/all-pi/autossh.sh > /home/pi/pi-appliance/autossh.sh

# supprime les ancien autossh
`ps -fe | grep "/usr/lib/autossh/autossh" | awk '{print "kill -9 " $2}'`


case $HOSTNAME in
  "vgm")       export AUTO_PORT=" -R 1301:*:22 " ;;
  "pi0")       export AUTO_PORT=" -R 1302:*:22 " ;;
  "pi2b")      export AUTO_PORT=" -R 1303:*:22 " ;;
  "pib2")      export AUTO_PORT=" -R 1303:*:22 " ;;
  "pi3")       export AUTO_PORT=" -R 1304:*:22 " ;;
  "pi3b")      export AUTO_PORT=" -R 1305:*:22 " ;;
  "pi4b")      export AUTO_PORT=" -R 1306:*:22 " ;;
  "pi3bp-vdl") export AUTO_PORT=" -R 1307:*:22 " ;;
  "pibp-vdl")  export AUTO_PORT=" -R 1308:*:22 " ;;
  "docker")    export AUTO_PORT=" -R 1309:*:22 " ;;
  *)       export AUTO_PORT="" ;;
esac

export LL=$LL" -L 2248:tom.moulard.org:22"
export LL=$LL" -L 2249:mbpdevrenaudin.home.pi-appliance.com:22"
export LL=$LL" -L 2250:awsb.moulard.org:22"


export AUTOSSH_DEBUG=yes
export AUTOSSH_LOGFILE=/var/log/autossh.log
export AUTOSSH_LOGLEVEL=5
export AUTO_OPTION=" -M 0 -q -f -N  -F /home/guillaume/.ssh/config "
autossh $AUTO_OPTION $AUTO_PORT $LL awsmb

