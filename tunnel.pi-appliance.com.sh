#!/bin/bash


export HOST_NAME=cloud.moulard.org
export HOST_PORT=1302
export HOST_USER=cloud

if [ `ps -ef | grep $HOST_PORT | grep $HOST_NAME | wc -l` -lt 1 ] ; then

   date
   echo lancement du tunnel
   ssh -T -R $HOST_PORT:*:22 $HOST_USER@$HOST_NAME sleep 365d
   echo fin tunnel sleep 365d
   date 

fi

CMD=`ssh pi@$HOST_NAME -p $HOST_PORT -o ConnectTimeout=5 echo OKTunnelUP` 
if [ '$CMD' != 'OKTunnelUP' ] ; then

    echo tunnel ko
    CMD=`ps -ef | grep $HOST_PORT | grep $HOST_NAME | awk '{print $2}'`
    kill -9 $CMD

else

    echo tunnel ok

fi

