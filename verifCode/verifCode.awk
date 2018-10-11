BEGIN {
	#	lstType (char, signed char, unsigned char, short, short int, signed short, signed short int, unsigned short, unsigned short int, int, signed, signed int, unsigned, unsigned int, long, long int, signed long, signed long int, unsigned long, unsigned long int, long long, long long int, signed long long, signed long long int, unsigned long long, unsigned long long int, float, double, long double )
	lstType ["char", "signed" , "unsigned" , "short", "int", "long", "float", "double", "struct" ,"size_t"];
    lstPreProcessorCmd = "if|else|endif|include|define|error|warning|pragma|undef";

	print "report verif code : ", ARGV[1]
	nbPbs=0;	
}

function reportBug (typeError, linNum, ligne) { 
	printf ("%3i %s20: %s\n",linNum, typeError, ligne) 
	nbPbs=nbPbs+1;
}

{
	
	if (length($0) >= 80) 
		reportBug("2.1-lg>80", NR, $0)
	
	if (index($0, "\t") > 0) 
		reportBug("2.2-TAB", NR, $0)

	if ((! match($0, "[a-zA-Z0-9}]+")) && (NR == 1))
		reportBug("2.5-BEGIN LINE EMPTY", NR, $0)

	if ( match($0, " $"))
		reportBug("2.7-ESPACE END LINE", NR, $0 "<")

		
	if ( match($0, "#") && match($0, lstPreProcessorCmd) && !match($0, "^#") )
			reportBug("5.1 #PreProc start #", NR, $0)
	
    if ( match(memoLigne, "#") && match(memoLigne, "else|endif") ) 
			if ( !match($0, "//"))
				reportBug("5.2 #PreProc else endif alwas comment", NR-1, memoLigne)

	tmp=$0
	if ( match(tmp, "{|}")){
		gsub(/ /,"",tmp)
		if (length(tmp) > 1)
			reportBug("6.2-BRACE OWN LINE", NR, tmp )
	}			
				
	if ( match($0, "{") )
		if (match($0, "{") != match (memoLigne, "[a-zA-Z0-9}]")){
			reportBug("6.3-BRACE START BAD COLUMN", NR, $0 )
				}
				
	#6.4 7.2 7.4 7.7 7.8 7.12 7.16 7.18 7.19 7.21 7.28 8.10
	#2.7 5.1 5.2 6.1 6.2 6.3 6.4 7.2 7.4 7.7 7.8 7.12 7.16 7.18 7.19 7.21 7.28 8.10

	memoLigne=$0
} 


END {
	if (! match($0, "[a-zA-Z0-9}]+"))
		reportBug("2.5-END LINE EMPTY", NR, $0)

	print "nbPbs: ", nbPbs
}
