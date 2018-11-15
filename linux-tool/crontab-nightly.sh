#!/bin/bash


sudo ~/pi-appliance/linux-tool/clean_log.sh

date

~/pi-appliance/linux-tool/bkp.sh

~/pi-appliance/linux-tool/dynHost/dynhost.sh
~/pi-appliance/linux-tool/clean_log.sh

#rsync -avz pi3://media/sda/Photo  /media/sda
#rsync -avz /media/sda/Photo pi3://media/sda


sudo /sbin/reboot
