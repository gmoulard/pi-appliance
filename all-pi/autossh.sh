#!/bin/bash
#
# description: setup autossh for gm pi cluster
# V0 : D/M/20 By guillaume@moulard.org - creation
#set -x

#  cat /sys/firmware/devicetree/base/model
# https://www.raspberrypi.org/documentation/hardware/raspberrypi/revision-codes/README.md
# $ sudo crontab -e
# @reboot /home/pi/pi-appliance/autossh.sh >> /var/log/pi-appliance.log 2>> /var/log/pi-appliance.err


case $HOSTNAME in
  "vgm")   export AUTO_PORT=" -R 1301:*:22 " ;;
  "pi0")   export AUTO_PORT=" -R 1302:*:22 " ;;
  "pi2b")  export AUTO_PORT=" -R 1303:*:22 " ;;
  "pib2")  export AUTO_PORT=" -R 1303:*:22 " ;;
  "pi3")   export AUTO_PORT=" -R 1304:*:22 " ;;
  "pi3b")  export AUTO_PORT=" -R 1305:*:22 " ;;
  "pi4b")  export AUTO_PORT=" -R 1306:*:22 " ;;
  "pi3bp-vdl") export AUTO_PORT=" -R 1307:*:22 " ;;
  "pibp-vdl")  export AUTO_PORT=" -R 1308:*:22 " ;;  
  *)       export AUTO_PORT="" ;;
esac

export AUTOSSH_DEBUG=yes
export AUTO_OPTION=" -M 0 -q -f -N  -oStrictHostKeyChecking=no -oServerAliveInterval=60 -oServerAliveCountMax=3 -i /home/pi/.ssh/identity "
autossh $AUTO_OPTION $AUTO_PORT cloud@vdlZZ.moulard.org
autossh $AUTO_OPTION $AUTO_PORT pi@pi.moulard.org

