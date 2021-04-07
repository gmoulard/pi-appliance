#!/bin/bash
#
# description: setup autossh for gm pi cluster
# V0 : 07/04/21 By guillaume@moulard.org - creation
#set -x

DEST=pi@pi3.moulard.org:/media/sda/bkpCloud/$HOSTNAME/

REPINFO=/tmp/info
rm -r $REPINFO
mkdir $REPINFO
crontab -l  > $REPINFO/crontab`date +%R`.txt
df -h  > $REPINFO/df`date +%R`.txt
ps -ef  > $REPINFO/ps`date +%R`.txt
ip a >  $REPINFO/pi`date +%R`.txt
ip route >>  $REPINFO/pi`date +%R`.txt


scp -r $REPINFO $DEST
scp /var/log/pi-appliance.* $DEST
scp -r /home/pi $DEST
sudo scp -r /root $DEST
