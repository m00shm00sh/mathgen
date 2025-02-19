#!/usr/bin/env python

#    xxxgen.py: main driver code
#
#    Copyright (C) 2020 Andrey V
#    This file is part of XXXgen.
#
#    Adapted from mathgen.pl from mathgen
#    (https://thatsmathematics.com/mathgen/). Portions may be copyright
#    (C) Nathaniel Eldredge.
#
#    XXXgen is free software; you can redistribute it and/or modify
#    it under the terms of the GNU General Public License as published by
#    the Free Software Foundation; either version 2 of the License, or
#    (at your option) any later version.
#
#    XXXgen is distributed in the hope that it will be useful,
#    but WITHOUT ANY WARRANTY; without even the implied warranty of
#    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
#    GNU General Public License for more details.
#
#    You should have received a copy of the GNU General Public License
#    along with XXXgen; if not, write to the Free Software
#    Foundation, Inc., 59 Temple Place, Suite 330, Boston, MA  02111-1307  USA

import logging
import sys
import xxxgen

# Activate the console logger
logger = logging.getLogger(None)
sh = logging.StreamHandler()
sh.setLevel(logging.DEBUG)
logger.addHandler(sh)

fh = None
if len(sys.argv) >= 2:
    fh = open(sys.argv[1], 'r')

g = xxxgen.Generator(start_token = 'START', stream = fh)
print(g.generate_string())
