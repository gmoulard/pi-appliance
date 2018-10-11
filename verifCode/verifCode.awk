BEGIN {
	print "report verif code : ", ARGV[1]
	nbPbs=0;
	
}

function reportBug (typeError, nr, ligne) { 
	print nr, ":", typeError, ":" ligne 
	nbPbs=nbPbs+1;
}

{
#2.1
if (length($0) >= 80) {
	reportBug("2.1-lg>80", NR, $0)
}   
#2.2
if (index($0, "\t") > 0) {
	reportBug("2.2-TAB", NR, $0)
#2.3

}   



}
END {
print "nbPbs: ", nbPbs
}
