#!/usr/bin/perl -w
#
# $Id: liblinkclean.pl,v 1.1 2003/01/10 01:19:17 erik Exp erik $
#
use Getopt::Std;
#

sub usage {
	print <<"EOF";
usage: $0 -s domain.com 
EOF
}

## Main
my $selfurl = "";
getopts("s:", \%opt);
if (defined($opt{h})) {usage(); exit 1;}
if (defined($opt{s}) && $opt{s} ne "") {
	$selfurl = $opt{s};
}
my $selfpattern = '.*' . $selfurl . '.*';

while (<>) {
	chomp;
	# 
	if (m/^\S+\s+\S+\s+\S+/) { # line has the right syntax
		if ( m/$selfpattern/ && m/\?/ ) { # site redircts via cgi 
			if (defined($opt{d})) {print STDERR "DEBUG found link contining a CGI redirect. $selfpattern\n";}
			#
			# All website related redirection of links goes here
			#
			if ( $selfurl eq "www.puppykibble.com") {
				#print STDERR "DEBUG puppykibble.com $0 used\n";
				s"www\.puppykibble\.com/cgi-bin/ucj/c\.cgi\?.*url="" ;
				s".*www\.puppykibble\.com/cgi-bin/ucj/c\.cgi\?.*link.*"" ;
			}
			elsif ( $selfurl eq "www.sexocean.com") {
				#print STDERR "DEBUG www.sexocean.com $0 used.\n";
				s"www\.sexocean\.com/cgi-bin/tt\.cgi\?url="" ;
				s".*www\.sexocean\.com/cgi-bin/tt\.cgi\?cmd=.*"" ;
			}
			# [one site entry removed for publication; see history/README.md]
			elsif ( $selfurl eq "www.picwarehouse.com") {
				#print STDERR "DEBUG www.picwarehouse.com $0 used.\n";
				s"www\.picwarehouse\.com/cgi-bin/ucj/c\.cgi\?url="" ;
			}
			elsif ( $selfurl eq "www.absolute-free-sexx.com") {
				#print STDERR "DEBUG www.absolute-free-sexx.com $0 used.\n";
				s"www\.absolute-free-sexx\.com/ttt-out\.php\?pct=80&url="" ;
				s"www\.absolute-free-sexx\.com/ttt-out\.php ="" ;
			}
			elsif ( $selfurl eq "www.findmodels.com") {
				#print STDERR "DEBUG www.findmodels.com $0 used.\n";
				my $regex1='www\.findmodels\.com/programs/extextlinkid1=';
				s"$regex1"" ;
			}
			elsif ( $selfurl eq "sex-mission.com") {
				#print STDERR "DEBUG sex-mission.com $0 used.\n";
				s"sex-mission\.com/outp\.php\?u="" ;
			}
			elsif ($selfurl eq "www.thumbzilla.com") {
				#print STDERR "DEBUG thumbzilla.com $0 used.\n";
				s"www\.thumbzilla\.com/cgi-bin/x3top3/outh\.cgi\?"";
			}
		}
		# 
		# link website unrelated deletion goes here. Like Counter.
		s/.*sextracker\.com.*//;
		s!cz8\.clickzs\.com/tgp\.php\?bbgx&75&CJ1&!!;
		s!cz8\.clickzs\.com/tx\.php\?bbgx&!!;
		s!http://alexmovies\.com/cgi-bin/ucj/c\.cgi\?url=!!;
		s!\S+\s+\S+(http://\S+)\s+\S+$!$1!;
		s!cz8\.clickzs\.com/t?\.php\?bbgx&!!;
		#s"http://secure.ibill.com/cgi-win/ccard/rscookie.exe\?RevShareID=\S+&returnto="";
		print "$_\n";
	}
}
