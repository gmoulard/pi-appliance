#!/bin/bash

repTL=/media/sda/timelaps
memoPiName=$1
memoDate=$2

cd $repTL 

if [ $# -eq 0 -o  $# -gt 2 ]
then 
     echo call: ./calltimelaps_day.sh piName date
     echo exemple : ./calltimelaps_day.sh piDebug mardi
fi

if [ $# -eq 1 ]
then 
     memoDate=`date +%A` 
fi

echo nbr parma:  $# 
echo memoPiName: $memoPiName
echo memoDate:   $memoDate

prepare () 
{
   echo $1
   rm $1/bw/*
   rm $1/th/*
   find $1/.. -type f -atime +4 -name pic* -delete
   ./reductionImage.py $rep
   ./compareDir.py $rep
}

calltimelaps()
{
echo Film $1
./timelaps_day.sh $rep $1.mp4 0.2 1
#./timelaps_day.sh $rep $1.full.mp4 0 1
./timelaps_day.sh $rep $1.speed.mp4 0.2 20 
#./timelaps_day.sh $rep $1.ZN3.mp4 0.3 1
#./timelaps_day.sh $rep $1.ZN4.mp4 0.4 1
}

rep=$repTL/$memoPiName/$memoDate
prepare $rep
calltimelaps $repTL/GMTimelaps/cloud.$memoPiName.`date +%j`.$memoDate
