#!/bin/bash

user=cloud
host=cloud.moulard.org
path=/home/cloud/pi-appliance/cmdCallBack/`hostname`/`whoami`
script=request


scp -C $user@$host:$path/$script.sh /tmp/$script.$$.sh
echo $#
if [ -f  /tmp/$script.$$.sh ]
then
chmod 777 /tmp/$script.$$.sh
/tmp/$script.$$.sh > /tmp/$script.$$.out 2> /tmp/$script.$$.err 
scp -C /tmp/$script.$$.* $user@$host:$path 
ssh $user@$host mv $path/$script.sh $path/$script.$$.sh.old
fi
