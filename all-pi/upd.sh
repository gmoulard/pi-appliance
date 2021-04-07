#!/bin/bash
#
# description: setup autossh for gm pi cluster
# V0 : 07/04/21 By guillaume@moulard.org - creation
#set -x
#  curl -s https://raw.githubusercontent.com/gmoulard/pi-appliance/master/all-pi/upd.sh | sh -
echo Begin: `date` 
cat /sys/firmware/devicetree/base/model

export REPO="https://raw.githubusercontent.com/gmoulard/pi-appliance/master/all-pi"

curl -s $REPO/autossh.sh > /home/pi/pi-appliance/autossh.sh
curl -s $REPO/bkp.sh > /home/pi/pi-appliance/bkp.sh

echo End: `date` 
