#!/bin/bash
#
# description: setup autossh for gm pi cluster
# V0 : 07/04/21 By guillaume@moulard.org - creation
#set -x

DEST=pi@pi.moulard.org:/media/sda/bkpCloud/$HOSTNAME/
TS=`date +%R`

REPINFO=/tmp/info
rm -r $REPINFO
mkdir $REPINFO
crontab -l  > $REPINFO/crontab${TS}.txt
df -h  > $REPINFO/df${TS}.txt
ps -ef  > $REPINFO/ps${TS}.txt


echo `hostname` >  $REPINFO/pi${TS}.txt
raspinfo >> $REPINFO/pi${TS}.txt
echo external IP: `/usr/bin/curl -s ipv4.icanhazip.com` >>  $REPINFO/pi${TS}.txt
ip a >>  $REPINFO/pi${TS}.txt
ip route >>  $REPINFO/pi${TS}.txt
echo

scp -r $REPINFO $DEST
scp /var/log/pi-appliance.* $DEST
scp -r /home/pi $DEST
sudo scp -r /root $DEST
