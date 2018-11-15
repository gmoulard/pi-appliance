#!/bin/bash

DEST=cloud@cloud.moulard.org:/data/backup/$HOSTNAME/
DEST=pi@pi3.moulard.org:/media/sda/bkpCloud/$HOSTNAME/

#see: http://duplicity.nongnu.org/features.html

REPINFO=/tmp/info
rm -r $REPINFO
mkdir $REPINFO
crontab -l  > $REPINFO/crontab`date +%R`.txt
df -h  > $REPINFO/df`date +%R`.txt
ps -ef  > $REPINFO/ps`date +%R`.txt


scp -r $REPINFO $DEST
scp /var/log/pi-appliance.* $DEST
scp -r /home/pi $DEST
sudo scp -r /root $DEST

