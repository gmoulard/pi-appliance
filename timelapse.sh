
#see: https://www.raspberrypi.org/documentation/usage/camera/raspicam/timelapse.md
#https://www.raspberrypi.org/documentation/raspbian/applications/camera.md
# add /dev/sda1     /media/sda1   ntfs   nofail,uid=1000,gid=100   0       0 on /etc/fstab


REP=/media/sda1/timelapse/$$
REP=/tmp/$$
TLFILE=$REP/tl_`date +"%a_%Hh"`

if [ `ps -ef | grep raspistill | wc -l` -eq 2 ] ; then
     echo raspistill up
     exit 0
fi

echo lancement de raspistill
mkdir $REP
/usr/bin/raspistill -t 3600000 -tl 30000 -o $REP/img_%04d.jpg

ls -tr $REP/img*.jpg > $REP/list_img.txt

mencoder -nosound \
     -ovc xvid  \
     -xvidencopts bitrate=900:max_bframes=0:quant_type=h263 \
     -vf scale=1920:1080 \
     -o $TLFILE.avi \
     -mf type=jpeg:fps=4 mf://@$REP/list_img.txt > $TLFILE.log 2> $TLFILE.err

ls -l $REP >> $TLFILE.log

#/usr/bin/ffprobe $TLFILE
DEST=cloud@cloud.moulard.org:/data/public/files/gmoulard/files/timeLapsPI/
DEST=cloud@cloud.moulard.org:/data/timeLaps/

sftp -C -oIdentityFile=~/.ssh/identity $DEST <<EOF
mput $TLFILE.avi*
quit
EOF

rm -r $REP


