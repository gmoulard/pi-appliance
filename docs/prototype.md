# Notes d'installation du prototype

Installation manuelle d'un Pi telle qu'elle se fait aujourd'hui, avant l'image pi-appliance (notes du 30 septembre 2025, reprises de l'ancien README).

```shell
imager-1.9.6.exe : setup wifi, clef public, user !!!
sudo apt-get update ; sudo apt-get upgrade -y ; sudo apt-get install autossh

mkdir  /home/pi/pi-appliance
# install autossh
curl https://raw.githubusercontent.com/gmoulard/pi-appliance/4ee13f9f0ef665e21388101aa5e06e20d3548c79/all-pi/autossh.sh > /home/pi/pi-appliance/autossh.sh
chmod 777 autossh.sh

# mise en place connection ssh
ssh-keygen -t rsa -b 4096
# copie clef public dans awsmb
ssh pi@awsmb -v

Lancement procees et validation
/home/pi/pi-appliance/autossh.sh
 ps -ef  | grep auto

ssh awsmb
netstat -paunt

# iot
crontab -e
15 * * * *  /usr/bin/libcamera-jpeg -o /tmp/libcamera-jpeg.jpg > /tmp/libcamera-jpeg.out 2> /tmp/libcamera-jpeg.err
```
