#!/bin/bash
#
# description: setup autossh for gm pi cluster
# V0 : 07/04/21 By guillaume@moulard.org - creation
#set -x
#  curl -s https://raw.githubusercontent.com/gmoulard/pi-appliance/master/all-pi/upd.sh | sh -
echo Begin: `date` 
cat /sys/firmware/devicetree/base/model

temp_file=$(mktemp)
REPO="https://raw.githubusercontent.com/gmoulard/pi-appliance/master/all-pi"

if [ `cat /sys/firmware/devicetree/base/model | grep Raspberry | wc -l` != 1 ]; then
    echo on est pas sur un pi, Pbs !!!!
    exit 0
fi

mkdir ~/pi-appliance
curl -s $REPO/upd.sh > ~/pi-appliance/upd.sh
curl -s $REPO/autossh.sh > ~/pi-appliance/autossh.sh
curl -s $REPO/bkp.sh > ~/pi-appliance/bkp.sh
curl -s $REPO/config > ~/.ssh/config

curl -s $REPO/authorized_keys >> ~/.ssh/authorized_keys
sort -u ~/.ssh/authorized_keys > ${temp_file}
cat ${temp_file} > ~/.ssh/authorized_keys

# crontab pi
# 1 1 * * * /home/pi/upd.sh 
#crontab root
#4 4 * * *  apt-get update ; apt-get -y upgrade ; reboot
#@reboot /home/pi/pi-appliance/autossh.sh >> /var/log/pi-appliance.log 2>> /var/log/pi-appliance.err

rm ${temp_file}
echo End: `date` 
