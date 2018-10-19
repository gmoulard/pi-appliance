

cd /tmp

rm index.html

/usr/bin/wget ipv4.icanhazip.com 

/usr/bin/scp -B -C /tmp/index.html  cloud@cloud.moulard.org:`hostname`.ip.txt


