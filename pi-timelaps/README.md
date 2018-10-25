

how to install 

'''


on crontab :

#Lauche movie creation
20 23 * * * /media/sda/timelaps/calltimelaps_day.sh pi0  >> /var/log/pi-appliance.log 2>> /var/log/pi-appliance.err



# lauche capture eatch 15 sec
* * * * * ( sleep 00 ; ~/pi-appliance/pi-timelaps/pictureToCloud.sh 1 >> /var/log/pi-appliance.log 2>> /var/log/pi-appliance.err )
* * * * * ( sleep 15 ; ~/pi-appliance/pi-timelaps/pictureToCloud.sh 2 >> /var/log/pi-appliance.log 2>> /var/log/pi-appliance.err )
* * * * * ( sleep 30 ; ~/pi-appliance/pi-timelaps/pictureToCloud.sh 3 >> /var/log/pi-appliance.log 2>> /var/log/pi-appliance.err )
* * * * * ( sleep 45 ; ~/pi-appliance/pi-timelaps/pictureToCloud.sh 4 >> /var/log/pi-appliance.log 2>> /var/log/pi-appliance.err )


for one day and one PI 16G is use by the pics

my hard disk tree:


/media/sda/timelaps
├── GMTimelaps
├── pi0
│   ├── dimanche
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── jeudi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── lundi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── mardi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── mercredi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── samedi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   └── vendredi
│       ├── bw
│       ├── img
│       ├── imgZeroNorm
│       └── th
├── pi3
│   ├── dimanche
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── jeudi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── lundi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── mardi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── mercredi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── samedi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   └── vendredi
│       ├── bw
│       ├── img
│       ├── imgZeroNorm
│       └── th
├── pi3b
│   ├── dimanche
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── jeudi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── lundi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── mardi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── mercredi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── samedi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   └── vendredi
│       ├── bw
│       ├── img
│       ├── imgZeroNorm
│       └── th
├── pib2
│   ├── dimanche
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── jeudi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── lundi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── mardi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── mercredi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   ├── samedi
│   │   ├── bw
│   │   ├── img
│   │   ├── imgZeroNorm
│   │   └── th
│   └── vendredi
│       ├── bw
│       ├── img
│       ├── imgZeroNorm
│       └── th
├── piDebug
│   ├── mardi
│   │   ├── bw
│   │   └── th
│   └── vendredi
│       ├── bw
│       └── th
├── piDemo
│   └── mardi
│       ├── bw
│       ├── img
│       ├── imgZeroNorm
│       └── th
'''
