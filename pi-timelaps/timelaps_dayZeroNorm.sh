#!/bin/bash

# exemple appel : ./timelaps_dayZeroNorm.sh /media/sda/timelaps/piDemo/mardi piDemo_mardi 0.2 &
echo repertoire photo $1
echo nom filme $rep/../GMTimelaps/cloudGM_`date +%j`_$2_ZeroNorm$3.mp4
echo Ratio ZeroNorm $2 


date
date >> /var/log/pi-appliance.err

rep=$1
cpt=1000000

rating=$2
cat $rep/compareDir.csv | awk -v zn=$3 '{FS=";"; if ($6 > zn ) {split($2,a,"/"); split(a[3],b,".jpg"); print b[1] }}' > $rep/zeroZone.lst
echo preparation liens depuis $rep
rm $rep/imgZeroNorm/img_*
for fic in $(cat $rep/zeroZone.lst ); do
  echo $rep/th/$fic.jpg_th.jpg $rep/imgZeroNorm/img_$cpt.jpg 
  cpt=`expr $cpt + 1` 
  ln -s $rep/th/$fic.jpg_th.jpg $rep/imgZeroNorm/img_$cpt.jpg
done  

echo ffmpeg -y -hide_banner -r 15 -start_number 1000001 -i $rep/imgZeroNorm/img_%d.jpg -s 1280*720 -vcodec libx264 $rep/../GMTimelaps/cloudGM_`date +%j`_$2_ZeroNorm$3.mp4
 

ffmpeg -y -hide_banner -r 15 -start_number 1000001 -i $rep/imgZeroNorm/img_%d.jpg -s 1280*720 -vcodec libx264 $rep/../GMTimelaps/cloudGM_`date +%j`_$2_ZeroNorm$3.mp4

date
echo --- stop timelaps.sh $1  ---
