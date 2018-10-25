#!/bin/bash


clik_and_push () {
   raspistill --exposure auto -o $1
   #echo raspistill return $? for $1
   cpt=0
   while ((cpt<10))
   do
     sleep 30
     #scp -C $1 pi@pi3.moulard.org:/media/sda/timelaps/`hostname`/`date +%A`/
     cp $1 /media/sda/timelaps/`hostname`/`date +%A`/
     if [[ $? -eq 0 ]]
     then
       cpt=100
     else
       ((cpt+=1))
     fi
   done
   #echo $1 $cpt
   rm $1
}


clik_and_push /tmp/pic_`date +%H_%M`_$1.jpg


