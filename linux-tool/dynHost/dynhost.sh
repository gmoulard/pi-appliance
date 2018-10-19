#! /bin/sh

# OVH - DynHost
#
# Permet de mettre à jour le champ DYNHOST
# pour votre nom de domaine.
# Utilise l'adresse de l'interface ppp0 de 
# votre système Linux.

# La mise à jour ne se fait que si l'adresse IP
# a effectivement changé.
# Fichier de log: dynhost.log

cd /home/pi/pi-appliance/dynHost

IFACE=eth0
HOST=`hostname`.moulard.org
LOGIN=moulard.org-admdyndns
PASSWORD=azeAZE123123
OPTIONS=""

getip() {
                ./cronMyIP.sh
		#IP=`/usr/bin/curl -s ipv4.icanhazip.com`
                IP=`cat /tmp/index.html`
                OLDIP=`cat ./old.ip`
        }
#

	echo ---------------------------------- >> ./dynhost.log
	echo `date` Demarrage de DynHost >> ./dynhost.log 
	getip

	if [ "$IP" ]; then
#		if [ "$OLDIP" != "$IP" ]; then
			echo -n "Ancienne IP: " >> ./dynhost.log
                	echo $OLDIP >> ./dynhost.log
               		echo -n "Nouvelle IP: " >> ./dynhost.log
              		echo $IP >> ./dynhost.log
			echo "Mise a jour!" >> ./dynhost.log
			if [ "$OPTIONS" =  "" ]; then 
				OPTIONS="-a $IP" 
#			fi
			
			echo ipcheck.py $OPTIONS $LOGIN $PASSWORD $HOST 
			python ipcheck.py $OPTIONS $LOGIN $PASSWORD $HOST >> ./dynhost.log
			echo -n "$IP" > ./old.ip				
         	else
               		echo IP Identique! OLDIP: $OLDIP IP: $IP Pas de mise à jour. >> ./dynhost.log
         	fi
         else
	 	echo Panique à bord: Aucune IP Disponible!! >> ./dynhost.log
         fi

