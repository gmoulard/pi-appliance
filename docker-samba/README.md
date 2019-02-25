see: https://github.com/dastrasmue/rpi-samba



# use samba images 
```
#pull

docker pull dastrasmue/rpi-samba


# run

docker stop samba; docker rm samba

docker run -d \
  -p 137:137/udp \
  -p 138:138/udp \
  -p 139:139 \
  -p 445:445 \
  -p 445:445/udp \
  --restart='always' \
  --hostname `hostname` \
  -v /:/share/pi3 \
  --name samba dastrasmue/rpi-samba:v3 \
  -u "guillaume:guillaume" \
  -s "guillaume (private):/share/pi3/var/www/html/DD:rw:" \
  -s "Public (readonly):/:ro:"
  
  
  
