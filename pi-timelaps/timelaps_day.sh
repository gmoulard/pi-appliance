#!/bin/bash
# exemple appel : ./timelaps_day.sh /media/sda/timelaps/piDebug/mardi /media/sda/timelaps/GMTimelaps/piDebug_mardi_0.2_1.mp4 0.2 1 
#                 ./timelaps_day.sh /media/sda/timelaps/piDemo/mardi  /media/sda/timelaps/GMTimelaps/pi3bmardi_0_4.mp4 0 4
#                 ./timelaps_day.sh /media/sda/timelaps/pi3b/mercredi /media/sda/timelaps/GMTimelaps/pi3bmercredi_0_1.mp4 0 1

rep=$1
rating=$3
cpt=1000000
filmName=$2
replink=/tmp/$$

echo --- Film               : $filmName
echo --- replink            : $replink/img_%d.jpg
echo --- repertoire photo   : $1
echo --- Ratio ZeroNorm     : $3
echo --- nombre images prise: 1 / $4



cat $rep/compareDir.csv | awk -v zn=$rating '{FS=";"; if ($6 > zn ) {nbr=split($2,a,"/"); split(a[nbr],b,".jpg"); print b[1] }}'  > $rep/zeroZone.lst

echo preparation liens depuis $rep
mkdir $replink
i=0
for fic in $(cat $rep/zeroZone.lst ); do
  if (( $i % $4 == 0 ))
  then
     cpt=`expr $cpt + 1`
     echo $i $rep/th/$fic.jpg_th.jpg $replink/img_$cpt.jpg
     ln -s $rep/th/$fic.jpg_th.jpg $replink/img_$cpt.jpg
     ((i=0))
  fi 
  ((i++))
done  


ffmpeg -y -hide_banner -r 15 -start_number 1000001 -i $replink/img_%d.jpg -s 1280*720 -vcodec libx264 $filmName

echo --- Film               : $filmName
echo --- replink            : $replink/img_%d.jpg
echo --- repertoire photo   : $1
echo --- Ratio ZeroNorm     : $3
echo --- nombre images prise: 1 / $4
date


