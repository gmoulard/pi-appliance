#!/usr/bin/env python
"""

"""
import sys
import os
import PIL 
import glob
from PIL import Image
from StringIO import StringIO

def main():
	   img = PIL.Image.open("/media/sda/timelaps/tmp/pic_11_12_1.jpg")
	   img.thumbnail((1280, 720))
	   dest = "/media/sda/timelaps/tmp/th.jpg"
	   img.save(dest, "JPEG", quality=100, optimize=True, progressive=True)
if __name__ == "__main__":
	main()

