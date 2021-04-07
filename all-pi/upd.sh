#!/bin/bash
#
# description: setup autossh for gm pi cluster
# V0 : 07/04/21 By guillaume@moulard.org - creation
#set -x
#  curl -s https://raw.githubusercontent.com/gmoulard/pi-appliance/master/all-pi/upd.sh | sh -
echo Begin: `date` 
cat /sys/firmware/devicetree/base/model
echo 

temp_file=$(mktemp)
REPO="https://raw.githubusercontent.com/gmoulard/pi-appliance/master/all-pi"

if [ `cat /sys/firmware/devicetree/base/model | grep Raspberry | wc -l` -ne 1 ]; then
    echo on est pas sur un pi, Pbs !!!!
    exit 0
fi

if [ ! -d ~/pi-appliance ]; then 
    mkdir ~/pi-appliance
fi

curl -s $REPO/upd.sh > ~/pi-appliance/upd.sh 
curl -s $REPO/autossh.sh > ~/pi-appliance/autossh.sh
curl -s $REPO/bkp.sh > ~/pi-appliance/bkp.sh
chmod 777  ~/pi-appliance/*.sh

curl -s $REPO/config > ~/.ssh/config

curl -s $REPO/authorized_keys >> ~/.ssh/authorized_keys
sort -u ~/.ssh/authorized_keys > ${temp_file}
cat ${temp_file} > ~/.ssh/authorized_keys

sudo crontab -l -u pi > ${temp_file}
if [ `grep -c upd.sh ${temp_file}` -ge 1 ]; then
    echo "1 1 * * * /home/pi/upd.sh"  >> ${temp_file}
fi
sudo crontab -u pi ${temp_file}

sudo crontab -l -u root > ${temp_file}
if [ `grep -c 'hostname..moulard' ${temp_file}` -ge 1 ]; then
    echo "02 * * * * /root/DynHost/dynhost \`hostname\`.moulard.org" >> ${temp_file}
fi
if [ `grep -c 'apt-get update' ${temp_file}` -ge 1 ]; then
    echo "4 4 * * *  apt-get update ; apt-get -y upgrade ; reboot" >> ${temp_file}
fi
if [ `grep -c 'autossh' ${temp_file}` -ge 1 ]; then
    echo "@reboot /home/pi/pi-appliance/autossh.sh >> /var/log/pi-appliance.log 2>> /var/log/pi-appliance.err" >> ${temp_file}
fi
sudo crontab -u root ${temp_file}


rm ${temp_file}
echo End: `date` 
