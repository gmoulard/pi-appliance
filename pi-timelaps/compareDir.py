#!/usr/bin/env python
"""Compare all aligned images of the same size.

"""

import sys
import os
import glob

from scipy.misc import imread
from scipy.linalg import norm
from scipy import sum, average

def main():
	memo_imgName = "nill"
	mon_fichier = open(sys.argv[1] + "/compareDir.csv", "w") 
        print "file: " + sys.argv[1] + "/compareDir.csv"
	#aff = "img1;img2;Manhattan norm;xel;Zero norm;/ per pixel;image size"
	#mon_fichier.write(aff + "\n")
	for imgName in sorted(glob.glob(sys.argv[1] + "/bw/*_th_bw.png" )):
                try:
		    img2 = imread(imgName).astype(float)
                except:
                    img2 = img1 
		if memo_imgName != "nill":
			n_m, n_0 = compare_images(img1, img2)
			aff = memo_imgName + ";"+imgName + ";" + str(n_m) + ";" + str(n_m/img1.size)+ ";" + str(n_0) + ";" + str(n_0*1.0/img1.size) + ";" + str(img1.size)
			mon_fichier.write(aff + "\n")
                        print aff
		memo_imgName = imgName
		img1 = img2
	mon_fichier.close()
		
			
			
def compare_images(img1, img2):
    # normalize to compensate for exposure difference
    img1 = normalize(img1)
    img2 = normalize(img2)
    # calculate the difference and its norms
    diff = img1 - img2  # elementwise for scipy arrays
    m_norm = sum(abs(diff))  # Manhattan norm
    z_norm = norm(diff.ravel(), 0)  # Zero norm
    return (m_norm, z_norm)

def normalize(arr):
    rng = arr.max()-arr.min()
    amin = arr.min()
    return (arr-amin)*255/rng

if __name__ == "__main__":
    main()

