#!/bin/bash

cd /media/sda/timelaps  

for pi in 'pi0' 'pi3' 'pi3b' 'pib2'  
do 
   for jour in 'vendredi' 'samedi' 'dimanche' 'lundi' 'mardi' 'mercredi' 'jeudi'
   do
        ./calltimelaps_day.sh $pi $jour
   done
done
 
