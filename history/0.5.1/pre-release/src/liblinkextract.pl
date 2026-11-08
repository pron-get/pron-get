#!/usr/bin/perl -w
#
# $Id: liblinkextract.pl,v 1.2 2003/03/03 02:55:39 erik Exp $
#
#use strict;
use vars qw(%linkdesc %linkurl %merged);

while (<>) {
	chomp;
	# find link number and description
	if (m/\[(\d+)\]([^[]*)/g) {
		$linknr = $1; $desc = $2;
		# now some beautifying
		$desc =~ s/\s/_/g;
		$desc =~ s/\W//g;
		$desc =~ s/_{2,}/_/g;
		$desc =~ s/_$//g;
		$desc =~ s/_(of|on|and|in|the|at|home|being|with|this|a|from)_/_/ig;
		# write to hash
		unless ($desc eq "" | $linknr eq "") {
			#print "DEBUG1 nr: $linknr desc: $desc\n";
			$linkdesc{$linknr} = $desc;
		}
	}
  # find link number and url
	if (m/^\s+(\d+)\.\s+http[s]*:\/\/(\S+)/) {
		$linknr = $1; $url = $2;
			#print "DEBUG1 nr: $linknr desc: $url\n";
			$linkurl{$linknr} = $url;
	}
}

# merge both hashes
#my $desc_num = keys %linkdesc;
#my $url_num = keys %linkurl;
#print "$desc_num \n";
#print "$url_num \n";
while (($key, $value) = each %linkdesc) {
  $merged{$value} = $linkurl{$key};
}

while (($desc, $url) = each %merged) {
	#unless ($url =~ /sextracker\.com|hitbox\.com/) {
		if ($desc =~ /movie/) {$url .= " .mp";} else {$url .= " .jp";}
		print "$desc $url\n";
	#}
}
