#!/bin/bash
#
# description: setup autossh for gm pi cluster
# V0 : 07/04/21 By guillaume@moulard.org - creation
#set -x
#  curl -s https://raw.githubusercontent.com/gmoulard/pi-appliance/master/all-pi/upd.sh | sh -

temp_file=$(mktemp)
REPO="https://raw.githubusercontent.com/gmoulard/pi-appliance/master/all-pi"

if [ `cat /sys/firmware/devicetree/base/model | grep Raspberry | wc -l` -ne 1 ]; then
    echo on est pas sur un Raspberry
fi

if [ ! -d ~/pi-appliance ]; then 
    mkdir ~/pi-appliance
fi

curl -s $REPO/upd.sh > ~/pi-appliance/upd.sh 
curl -s $REPO/autossh.sh > ~/pi-appliance/autossh.sh
curl -s $REPO/bkp.sh > ~/pi-appliance/bkp.sh
chmod 777  ~/pi-appliance/*.sh

curl -s $REPO/crontab-root > ~/pi-appliance/crontab-root
curl -s $REPO/crontab-pi > ~/pi-appliance/crontab-pi


curl -s $REPO/vimrc > ~/.vimrc
sudo cp ~/.vimrc /root/.vimrc

curl -s $REPO/bash_aliases > ~/.bash_aliases

curl -s $REPO/config > ~/.ssh/config

curl -s $REPO/authorized_keys >> ~/.ssh/authorized_keys
sort -u ~/.ssh/authorized_keys > ${temp_file}
cat ${temp_file} > ~/.ssh/authorized_keys
