#!/bin/bash

date

~/pi-appliance/linux-tool/bkp.sh

~/pi-appliance/linux-tool/dynHost/dynhost.sh
~/pi-appliance/linux-tool/clean_log.sh
rsync -vrhz --progress pi3:/var/www/html/DD/Photo/ /var/www/html/DD/Photo/

sudo /sbin/reboot
