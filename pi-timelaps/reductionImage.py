#!/usr/bin/env python

import sys
import os
import PIL
import glob
 
from PIL import Image
from StringIO import StringIO

def main():
	for imgName in sorted(glob.glob(sys.argv[1] + "/pic*.jpg" )):
           try: 
	       img = PIL.Image.open(imgName)
	       img.thumbnail((1280, 720))
	       dest = imgName.replace( sys.argv[1], sys.argv[1] + "/th" ) + "_th.jpg"
	       img.save(dest, "JPEG", quality=100, optimize=True, progressive=True)
               print "save : " + dest    
	       img = img.convert('LA')
	       dest = imgName.replace( sys.argv[1], sys.argv[1] + "/bw" ) + "_th_bw.png"
               print "save : " + dest
	       img.save(dest)
	       print dest
           except:
               print "probleme with " + imgName + ": ",  sys.exc_info()[0] 

if __name__ == "__main__":
	main()

