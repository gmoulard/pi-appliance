#!/bin/bash

# Max2Play WPS (Wifi Protected Setup) Auto Connection on Startup
# Just make sure to activate WPS on Router before booting up your device
# Automatically sets up and saves WiFi Connection

# only run if there's no current wifi connection and WiFi is enabled
if [ "$(LANG=C && /sbin/ifconfig wlan0 | grep 'HWaddr\|ether' | wc -l)" -gt "0" -a "$(LANG=C && /sbin/ip addr show wlan0 | grep 'inet ' | grep -v '169.254' | wc -l)" -lt "1" ]; then
    killall -q wpa_supplicant
    sleep 1
    # Check if "update_config=1" needed in /opt/max2play/wpa_supplicant.conf for Autoconfig
    if [ "$(grep -i "update_config=1" /opt/max2play/wpa_supplicant.conf | wc -l)" -lt "1" ]; then
    	echo "update_config=1" >> /opt/max2play/wpa_supplicant.conf
    fi
    
    # Make sure WPA-Supplicant is running with config
    # separate RPI3 no wext Driver for WPS!
    if [ "0" -lt "$(wpa_supplicant -h | grep nl80211 | wc -l)" ]; then
    	wpa_supplicant -B w -i wlan0 -c /opt/max2play/wpa_supplicant.conf
    else
    	wpa_supplicant -B w -D wext -i wlan0 -c /opt/max2play/wpa_supplicant.conf
    fi
    sleep 3
    
    # Clear network list
    for i in `wpa_cli -iwlan0 list_networks | grep ^[0-9] | cut -f1`; do wpa_cli -iwlan0 remove_network $i; done
    
    # get Routers supporting WPS, sorted by signal strength        
    SSID=$(/sbin/wpa_cli -iwlan0 scan_results | grep "WPS" | sort -r -k3 | awk 'END{print $NF}')
    echo "Using $SSID for WPS"
    #SUCCESS=$(wpa_cli -iwlan0 wps_pbc $SSID)
    SUCCESS=$(wpa_cli -iwlan0 wps_pbc)
    sleep 10
    
    # Check for Entry in wpa_supplicant.conf
    VALIDENTRY=$(grep -i "^network=" /opt/max2play/wpa_supplicant.conf | wc -l)
    
    # wpa_supplicant.conf should be modified in last 20 seconds by WPS Config
    MODIFIED=$(( `date +%s` - `stat -L --format %Y /opt/max2play/wpa_supplicant.conf` ))
    
    if [ "$(echo "$SUCCESS" | grep 'OK' | wc -l)" -gt "0" -a "$VALIDENTRY" -gt "0" -a "$MODIFIED" -lt "20" ]; then
    	# Now Config File should be written    	
    	
    	# Stop existing WPA_Supplicant Process with Old Config
    	killall -q wpa_supplicant
    	sleep 3
    	
    	# Enable wlan0 in /etc/network/interfaces
    	if [ "$(grep -i '^auto wlan0' /etc/network/interfaces | wc -l)" -lt "1" ]; then
    		sed -i "s/#allow-hotplug wlan0/allow-hotplug wlan0/;s/#iface wlan0 inet dhcp/iface wlan0 inet dhcp/;s/#auto wlan0/auto wlan0/;s/#pre-up wpa_supplicant/pre-up wpa_supplicant/;s/#post-down killall -q wpa_supplicant/post-down killall -q wpa_supplicant/" /etc/network/interfaces  	
		fi
		    	
    	# Startup wlan0 with new Config
    	wpa_action wlan0 stop
        wpa_action wlan0 reload        
    	/sbin/ifup wlan0
    	
    	# Activate Auto-Reconnect on Loosing Connection
    	autoreconnect_wifi=$(cat /opt/max2play/options.conf | grep autoreconnect_wifi | wc -l)    
	    if [ "1" -gt "$autoreconnect_wifi" ]; then
	        echo "autoreconnect_wifi=1" >> /opt/max2play/options.conf
	    else
	    	sed -i 's/autoreconnect_wifi.*/autoreconnect_wifi=1/' /opt/max2play/options.conf
	    fi
	    
    else
    	echo "ERROR creating connection"
    	echo $SUCCESS
    fi
fi
