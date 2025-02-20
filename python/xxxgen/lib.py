#    xxxgen.py: XXXgen grammar engine
#
#    Copyright (C) 2020 Andrey V
#                  2012  Nathaniel Eldredge
#
#    Adapted from scigen.pm from mathgen
#    (https://thatsmathematics.com/mathgen/). Portions may be copyright
#    (C) Nathaniel Eldredge.
#
#    Adapted from scigen.pl from SCIgen
#    (http://pdos.csail.mit.edu/scigen/).  Portions may be copyright
#    (C) the authors of SCIgen.
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
from collections import namedtuple
from pprint import pprint
import random
import re
from sys import stdin
import time

class Generator:
    def __init__(self, *, start_token = None, stream = None, log_level = logging.DEBUG):
        self._logger = logging.getLogger('xxxgen')
        self._logger.setLevel(log_level)

        # read rule file from io-ish arg `stream`
        self._rules: dict[str, list[str]] = {}
        self._num_rules: dict[str, int] = {}
        self._dup_rules: dict[str, list[str]] = {}
        self._did_files = set()
        self._child_fhs = []
        # empty stream -> read from stdin
        self._read_file(stream if stream else stdin)
        # clean up open child files
        for fh in self._child_fhs: fh.close()
        self._child_fhs.clear()
        self._did_files.clear()

        self._lookup = Generator._make_lookup_rx(self._rules)

        self._start_token = start_token


    #
    # Methods for parsing the rule files
    #

    # We abstract reading a line from a stream to a generator for two reasons:
    # (1) filter comment and empty lines
    # (2) allow sharing the notion of next line between outer and inner loop;
    #     this can be done with either a file or generator but generator feels cleaner
    def _normalized_lines(stream):
        for raw_line in stream:
            line = raw_line.strip()
            if not line or line[0] == '#':
                continue
            if line:
                yield line

    def _read_file(self, stream):
        lines = Generator._normalized_lines(stream)
        for line in lines:
            words = line.split()
            name = words.pop(0)

            # non-duplicate rule
            # this means that on expansion, it will choose a different
            # substitution for each instance
            m = re.search(r'([^\+]*)\!$', name)
            if m:
                name = m.group(1)
                self._dup_rules.setdefault(name, []).append('')
                continue
            # include rule
            if name == '.include':
                # make sure we haven't already included this file
                # NOTE: this allows the main file to be included at most twice
                if words[0] in self._did_files:
                    self._logger.warning(f'Skipping duplicate file {words[0]}')
                    continue
                # implicit throw if open fails
                self._child_fhs.append(open(words[0], 'r'))
                self._did_files.add(words[0])
                self._read_file(self._child_fhs[-1])
                continue # we don't want to have .include itself be a rule
                
            rule = ''

            # multi-line rule
            if len(words) == 1 and words[0] == '{':
                seen_end = False
                # continue using the generator from the outer loop
                for line in lines:
                    if line == '}':
                        seen_end = True
                        break
                    else:
                        # We will have to lstrip the newline from rule.
                        # This may be inefficient.
                        rule = rule + "\n" + line
                if not seen_end:
                    raise ValueError("end of file reached before matching '}' found")
            else:
                rule = ' '.join(words)

            # look for the weight
            weight = 1
            m = re.search(r'([^\+]*)\+(\d+)$', name)
            if m:
                name = m.group(1)
                weight = int(m.group(2))
                self._logger.debug(f'weighing rule by {weight}: {name} -> {rule}')

            self._rules.setdefault(name, []).extend((rule.lstrip(),) * weight)

        self._rules['AUTHOR_NAME'] = [ 'AUTHOR' ]
        self._rules['SCI_YEAR'] = Generator._do_year_rule()
        
    def _make_lookup_rx(rules):
        # the lookup pattern RE is populated by descending key length
        # this may lead to an inefficient pattern compared to using a trie
        # to derive the optimal RE
        pat = '|'.join(sorted(rules.keys(), key = lambda k: -1 * len(k)))
        return re.compile(fr'^(.*?)({pat})', flags = re.S)
        
    def _do_year_rule():

        year = time.localtime()[0]

        # We wish to have entries for each of the last 100 years, with
        # more recent years being exponentially more likely
        nyears = 100
        r = 35 # newest year is this many times more likely than oldest

        year_rule = []

        # don't use current year
        for i in range(nyears):
            y = year - nyears + i
            n = r ** (i / nyears)
            year_rule.extend((str(y),)*int(n))
        
        return year_rule
    # 
    # Methods for generating the string
    #

    def add_authors(self, *alist):
        self._rules["AUTHOR_NAME"] = alist
        a, lastauth = alist[:-1], alist[-1]
        s = ''
        if a:
            s = ", ".join(a) + " and "
        s = s + lastauth
        self._rules['SCIAUTHORS'] = [ s ]
        # rebuild
        self._lookup = Generator._make_lookup_rx(self._rules)


    _pfr_result = namedtuple('Pop_first_rule_result',
                               ['preamble', 'rule', 'post_input']
                             )

    def _pop_first_rule(self, in_tok):
        # Use a nested function as a callback to capture match state
        pre, rule, did_match = (None,) * 3
        def rep_fn(match):
            nonlocal pre, rule, did_match
            pre = match.group(1)
            rule = match.group(2)
            did_match = True
            return ''
        new_tok = self._lookup.sub(rep_fn, in_tok, count = 1)
        if did_match:
            return self._pfr_result(pre, rule, new_tok)
        return None
    
    def generate_string(self):
        s = self._generate_string_r(self._start_token)
        # cleanup
        self._num_rules = {}
        return s

    def _generate_string_r(self, start_tok):
        
        # check for special rules ending in + and # 
        # Rules ending in + generate a sequential integer
        # The same rule ending in # chooses a random # from among preiously
        # generated integers
        # The stripped rule entry is the active counter which is used as either
        # a thing to increment or an upper limit
        m = re.search(r'(.*)([\+#])$', start_tok)
        if m:
            rule_n = m.group(1).strip()
            i = self._num_rules.get(rule_n, 0)
            if m.group(2) == '+':
                self._num_rules[rule_n] = i + 1
            elif m.group(2) == '#' and bool(i):
                i = random.randrange(i)
            return str(i)

        full_token = None
        do_repeat = True
        count = 0
        while do_repeat:
            in_tok = random.choice(self._rules[start_tok])
            count += 1
            self._logger.debug(f'{start_tok} -> {in_tok}')

            do_repeat = None

            # recursively expand until rule matches are exhausted
            components = []

            while True:
                _pfr = self._pop_first_rule(in_tok)
                if _pfr is None:
                    break
                (pre, rule, in_tok) = _pfr
                ex = self._generate_string_r(rule)
                if pre:
                    components.append(pre)
                if ex:
                    components.append(ex)

                _pfr = self._pop_first_rule(in_tok)
            if in_tok:
                components.append(in_tok)
            full_token = "".join(components)
            
            # check for dup name
            dups = self._dup_rules.get(start_tok)

            if dups:
                # make sure we haven't generated this exact token yet
                for d in dups:
                    if d == full_token:
                        do_repeat = True
                        break
                
                if not do_repeat:
                    self._dup_rules[start_tok].append(full_token)
                elif count > 50:
                    do_repeat = False

        return full_token

