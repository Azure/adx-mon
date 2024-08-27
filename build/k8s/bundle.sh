#!/bin/sh
# This is a shell archive (produced by GNU sharutils 4.15.2).
# To extract the files from this archive, save it to some FILE, remove
# everything before the '#!/bin/sh' line above, then type 'sh FILE'.
#
lock_dir=_sh00445
# Made on 2024-08-27 05:06 UTC by <root@d8985da8c53e>.
# Source directory was '/build'.
#
# Existing files WILL be overwritten.
#
# This shar contains:
# length mode       name
# ------ ---------- ------------------------------------------
#   5941 -rwxr-xr-x setup.sh
#   4377 -rw-r--r-- ingestor.yaml
#   5804 -rw-r--r-- ksm.yaml
#   6999 -rw-r--r-- collector.yaml
#
MD5SUM=${MD5SUM-md5sum}
f=`${MD5SUM} --version | egrep '^md5sum .*(core|text)utils'`
test -n "${f}" && md5check=true || md5check=false
${md5check} || \
  echo 'Note: not verifying md5sums.  Consider installing GNU coreutils.'
if test "X$1" = "X-c"
then keep_file=''
else keep_file=true
fi
echo=echo
save_IFS="${IFS}"
IFS="${IFS}:"
gettext_dir=
locale_dir=
set_echo=false

for dir in $PATH
do
  if test -f $dir/gettext \
     && ($dir/gettext --version >/dev/null 2>&1)
  then
    case `$dir/gettext --version 2>&1 | sed 1q` in
      *GNU*) gettext_dir=$dir
      set_echo=true
      break ;;
    esac
  fi
done

if ${set_echo}
then
  set_echo=false
  for dir in $PATH
  do
    if test -f $dir/shar \
       && ($dir/shar --print-text-domain-dir >/dev/null 2>&1)
    then
      locale_dir=`$dir/shar --print-text-domain-dir`
      set_echo=true
      break
    fi
  done

  if ${set_echo}
  then
    TEXTDOMAINDIR=$locale_dir
    export TEXTDOMAINDIR
    TEXTDOMAIN=sharutils
    export TEXTDOMAIN
    echo="$gettext_dir/gettext -s"
  fi
fi
IFS="$save_IFS"
f=shar-touch.$$
st1=200112312359.59
st2=123123592001.59
st2tr=123123592001.5 # old SysV 14-char limit
st3=1231235901

if   touch -am -t ${st1} ${f} >/dev/null 2>&1 && \
     test ! -f ${st1} && test -f ${f}; then
  shar_touch='touch -am -t $1$2$3$4$5$6.$7 "$8"'

elif touch -am ${st2} ${f} >/dev/null 2>&1 && \
     test ! -f ${st2} && test ! -f ${st2tr} && test -f ${f}; then
  shar_touch='touch -am $3$4$5$6$1$2.$7 "$8"'

elif touch -am ${st3} ${f} >/dev/null 2>&1 && \
     test ! -f ${st3} && test -f ${f}; then
  shar_touch='touch -am $3$4$5$6$2 "$8"'

else
  shar_touch=:
  echo
  ${echo} 'WARNING: not restoring timestamps.  Consider getting and
installing GNU '\''touch'\'', distributed in GNU coreutils...'
  echo
fi
rm -f ${st1} ${st2} ${st2tr} ${st3} ${f}
#
if test ! -d ${lock_dir} ; then :
else ${echo} "lock directory ${lock_dir} exists"
     exit 1
fi
if mkdir ${lock_dir}
then ${echo} "x - created lock directory ${lock_dir}."
else ${echo} "x - failed to create lock directory ${lock_dir}."
     exit 1
fi
# ============= setup.sh ==============
  sed 's/^X//' << 'SHAR_EOF' | uudecode &&
begin 600 setup.sh
M(R$O8FEN+V)A<V@*<V5T("UE=6\@<&EP969A:6P*4T-225!47T1)4CTB)"AD
M:7)N86UE("(D>T)!4TA?4T]54D-%6S!=?2(I(@H*"FEF("$@8V]M;6%N9"`M
M=B!A>B`F/B`O9&5V+VYU;&P*=&AE;@H@("`@96-H;R`B5&AE("=A>B<@8V]M
M;6%N9"!C;W5L9"!N;W0@8F4@9F]U;F0N(%!L96%S92!I;G-T86QL($%Z=7)E
M($-,22!B969O<F4@8V]N=&EN=6EN9RXB"B`@("!E>&ET"F9I"@II9B`A(&%Z
M(&%C8V]U;G0@<VAO=R`F/B`O9&5V+VYU;&P[('1H96X*("`@(&5C:&\@(EEO
M=2!A<F4@;F]T(&QO9V=E9"!I;B!T;R!!>G5R92!#3$DN(%!L96%S92!L;V<@
M:6XN(@H@("`@87H@;&]G:6X*9FD*"E1/2T5.7T584$E263TD*&%Z(&%C8V]U
M;G0@9V5T+6%C8V5S<RUT;VME;B`M+7%U97)Y(&5X<&ER97-?;VX@+6\@='-V
M*0I#55)214Y47T1!5$4])"AD871E("UU("LE<RD*"FEF(%M;("(D0U524D5.
M5%]$051%(B`^("(D5$]+14Y?15A025)9(B!=73L@=&AE;@H@("`@96-H;R`B
M66]U<B!!>G5R92!#3$D@=&]K96X@:&%S(&5X<&ER960N(%!L96%S92!L;V<@
M:6X@86=A:6XN(@H@("`@87H@;&]G:6X*9FD*"FEF("$@87H@97AT96YS:6]N
M('-H;W<@+2UN86UE(')E<V]U<F-E+6=R87!H("8^("]D978O;G5L;#L@=&AE
M;@H@("`@<F5A9"`M<"`B5&AE("=R97-O=7)C92UG<F%P:"<@97AT96YS:6]N
M(&ES(&YO="!I;G-T86QL960N($1O('EO=2!W86YT('1O(&EN<W1A;&P@:70@
M;F]W/R`H>2]N*2`B($E.4U1!3$Q?4D<*("`@(&EF(%M;("(D24Y35$%,3%]2
M1R(@/3T@(GDB(%U=.R!T:&5N"B`@("`@("`@87H@97AT96YS:6]N(&%D9"`M
M+6YA;64@<F5S;W5R8V4M9W)A<&@*("`@(&5L<V4*("`@("`@("!E8VAO(")4
M:&4@)W)E<V]U<F-E+6=R87!H)R!E>'1E;G-I;VX@:7,@<F5Q=6ER960N($5X
M:71I;F<N(@H@("`@("`@(&5X:70@,0H@("`@9FD*9FD*"B,@07-K(&9O<B!T
M:&4@;F%M92!O9B!T:&4@86MS(&-L=7-T97(@86YD(')E860@:70@87,@:6YP
M=70N("!7:71H('1H870@;F%M92P@<G5N(&$@9W)A<&@@<75E<GD@=&\@9FEN
M9`IR96%D("UP(")0;&5A<V4@96YT97(@=&AE(&YA;64@;V8@=&AE($%+4R!C
M;'5S=&5R('=H97)E($%$6"U-;VX@8V]M<&]N96YT<R!S:&]U;&0@8F4@9&5P
M;&]Y960Z("(@0TQ54U1%4@IW:&EL92!;6R`M>B`B)'M#3%535$52+R\@?2(@
M75T[(&1O"B`@("!E8VAO(")#;'5S=&5R(&-A;FYO="!B92!E;7!T>2X@4&QE
M87-E(&5N=&5R('1H92!N86UE(&]F('1H92!!2U,@8VQU<W1E<CHB"B`@("!R
M96%D($-,55-415(*9&]N90H*(R!2=6X@82!G<F%P:"!Q=65R>2!T;R!F:6YD
M('1H92!C;'5S=&5R)W,@<F5S;W5R8V4@9W)O=7`@86YD('-U8G-C<FEP=&EO
M;B!I9`I#3%535$527TE.1D\])"AA>B!G<F%P:"!Q=65R>2`M<2`B4F5S;W5R
M8V5S('P@=VAE<F4@='EP92`]?B`G36EC<F]S;V9T+D-O;G1A:6YE<E-E<G9I
M8V4O;6%N86=E9$-L=7-T97)S)R!A;F0@;F%M92`]?B`G)$-,55-415(G('P@
M<')O:F5C="!R97-O=7)C94=R;W5P+"!S=6)S8W)I<'1I;VY)9"P@;&]C871I
M;VXB*0II9B!;6R`D*&5C:&\@)$-,55-415)?24Y&3R!\(&IQ("<N9&%T82!\
M(&QE;F=T:"<I("UE<2`P(%U=.R!T:&5N"B`@("!E8VAO(").;R!!2U,@8VQU
M<W1E<B!C;W5L9"!B92!F;W5N9"!F;W(@=&AE(&-L=7-T97(@;F%M92`G)$-,
M55-415(G+B!%>&ET:6YG+B(*("`@(&5X:70@,0IF:0H*4D533U520T5?1U)/
M55`])"AE8VAO("1#3%535$527TE.1D\@?"!J<2`M<B`G+F1A=&%;,%TN<F5S
M;W5R8V5'<F]U<"<I"E-50E-#4DE05$E/3E])1#TD*&5C:&\@)$-,55-415)?
M24Y&3R!\(&IQ("UR("<N9&%T85LP72YS=6)S8W)I<'1I;VY)9"<I"E)%1TE/
M3CTD*&5C:&\@)$-,55-415)?24Y&3R!\(&IQ("UR("<N9&%T85LP72YL;V-A
M=&EO;B<I"@HC($9I;F0@=&AE(&UA;F%G960@:61E;G1I='D@8VQI96YT($E$
M(&%T=&%C:&5D('1O('1H92!!2U,@;F]D92!P;V]L<PI.3T1%7U!/3TQ?241%
M3E1)5%D])"AA>B!A:W,@<VAO=R`M+7)E<V]U<F-E+6=R;W5P("1215-/55)#
M15]'4D]54"`M+6YA;64@)$-,55-415(@+2UQ=65R>2!I9&5N=&ET>5!R;V9I
M;&4N:W5B96QE=&ED96YT:71Y+F-L:65N=$ED("UO('1S=BD*"F5C:&\*96-H
M;R`M92`B1F]U;F0@04M3(&-L=7-T97(@:6YF;SHB"F5C:&\@+64@(B`@04M3
M($-L=7-T97(@3F%M93H@7&5;,S)M)$-,55-415)<95LP;2(*96-H;R`M92`B
M("!297-O=7)C92!'<F]U<#H@7&5;,S)M)%)%4T]54D-%7T=23U507&5;,&TB
M"F5C:&\@+64@(B`@4W5B<V-R:7!T:6]N($E$.EQE6S,R;2`D4U5"4T-225!4
M24].7TE$7&5;,&TB"F5C:&\@+64@(B`@4F5G:6]N.B!<95LS,FTD4D5'24].
M7&5;,&TB"F5C:&\@+64@(B`@36%N86=E9"!)9&5N=&ET>2!#;&EE;G0@240Z
M(%QE6S,R;21.3T1%7U!/3TQ?241%3E1)5%E<95LP;2(*96-H;PIR96%D("UP
M("))<R!T:&ES(&EN9F]R;6%T:6]N(&-O<G)E8W0_("AY+VXI("(@0T].1DE2
M30II9B!;6R`B)$-/3D9)4DTB("$](")Y(B!=73L@=&AE;@H@("`@96-H;R`B
M17AI=&EN9R!A<R!T:&4@:6YF;W)M871I;VX@:7,@;F]T(&-O<G)E8W0N(@H@
M("`@97AI="`Q"F9I"@IA>B!A:W,@9V5T+6-R961E;G1I86QS("TM<W5B<V-R
M:7!T:6]N("1354)30U))4%1)3TY?240@+2UR97-O=7)C92UG<F]U<"`D4D53
M3U520T5?1U)/55`@+2UN86UE("1#3%535$52"@IE8VAO"G)E860@+7`@(E!L
M96%S92!E;G1E<B!T:&4@07IU<F4@1&%T82!%>'!L;W)E<B!C;'5S=&5R(&YA
M;64@=VAE<F4@0418+4UO;B!W:6QL('-T;W)E('1E;&5M971R>3H@(B!#3%53
M5$527TY!344*=VAI;&4@6UL@+7H@(B1[0TQ54U1%4E].04U%+R\@?2(@75T[
M(&1O"B`@("!E8VAO(")!1%@@8VQU<W1E<B!N86UE(&-A;FYO="!B92!E;7!T
M>2X@4&QE87-E(&5N=&5R('1H92!D871A8F%S92!N86UE.B(*("`@(')E860@
M0TQ54U1%4E].04U%"F1O;F4*"D-,55-415)?24Y&3STD*&%Z(&=R87!H('%U
M97)Y("UQ(")297-O=7)C97,@?"!W:&5R92!T>7!E(#U^("=-:6-R;W-O9G0N
M2W5S=&\O8VQU<W1E<G,G(&%N9"!N86UE(#U^("<D0TQ54U1%4E].04U%)R!\
M('!R;VIE8W0@;F%M92P@<F5S;W5R8V5'<F]U<"P@<W5B<V-R:7!T:6]N260L
M(&QO8V%T:6]N+"!P<F]P97)T:65S+G5R:2(I"D-,55-415)?0T]53E0])"AE
M8VAO("1#3%535$527TE.1D\@?"!J<2`G+F1A=&$@?"!L96YG=&@G*0H*:68@
M6UL@)$-,55-415)?0T]53E0@+65Q(#`@75T[('1H96X*("`@(&5C:&\@(DYO
M($MU<W1O(&-L=7-T97(@8V]U;&0@8F4@9F]U;F0@9F]R('1H92!D871A8F%S
M92!N86UE("<D0TQ54U1%4E].04U%)RX@17AI=&EN9RXB"B`@("!E>&ET(#$*
M96QS90H@("`@0TQ54U1%4E].04U%/20H96-H;R`D0TQ54U1%4E])3D9/('P@
M:G$@+7(@)RYD871A6S!=+FYA;64G*0H@("`@4U5"4T-225!424].7TE$/20H
M96-H;R`D0TQ54U1%4E])3D9/('P@:G$@+7(@)RYD871A6S!=+G-U8G-C<FEP
M=&EO;DED)RD*("`@(%)%4T]54D-%7T=23U50/20H96-H;R`D0TQ54U1%4E])
M3D9/('P@:G$@+7(@)RYD871A6S!=+G)E<V]U<F-E1W)O=7`G*0H@("`@0418
M7T911$X])"AE8VAO("1#3%535$527TE.1D\@?"!J<2`M<B`G+F1A=&%;,%TN
M<')O<&5R=&EE<U]U<FDG*0IF:0IE8VAO"F5C:&\@(D9O=6YD($%$6"!C;'5S
M=&5R(&EN9F\Z(@IE8VAO("UE("(@($-L=7-T97(@3F%M93H@7&5;,S)M)$-,
M55-415)?3D%-15QE6S!M(@IE8VAO("UE("(@(%-U8G-C<FEP=&EO;B!)1#H@
M7&5;,S)M)%-50E-#4DE05$E/3E])1%QE6S!M(@IE8VAO("UE("(@(%)E<V]U
M<F-E($=R;W5P.B!<95LS,FTD4D533U520T5?1U)/55!<95LP;2(*96-H;R`M
M92`B("!!1%@@1E%$3CH@7&5;,S)M)$%$6%]&441.7&5;,&TB"F5C:&\*<F5A
M9"`M<"`B27,@=&AI<R!T:&4@8V]R<F5C="!!1%@@8VQU<W1E<B!I;F9O/R`H
M>2]N*2`B($-/3D9)4DT*:68@6UL@(B1#3TY&25)-(B`A/2`B>2(@75T[('1H
M96X*("`@(&5C:&\@(D5X:71I;F<@87,@=&AE($%$6"!C;'5S=&5R(&EN9F\@
M:7,@;F]T(&-O<G)E8W0N(@H@("`@97AI="`Q"F9I"@IF;W(@1$%404)!4T5?
M3D%-12!I;B!-971R:6-S($QO9W,[(&1O"B`@("`C($-H96-K(&EF('1H92`D
M1$%404)!4T5?3D%-12!D871A8F%S92!E>&ES=',*("`@($1!5$%"05-%7T58
M25-44STD*&%Z(&MU<W1O(&1A=&%B87-E('-H;W<@+2UC;'5S=&5R+6YA;64@
M)$-,55-415)?3D%-12`M+7)E<V]U<F-E+6=R;W5P("1215-/55)#15]'4D]5
M4"`M+61A=&%B87-E+6YA;64@)$1!5$%"05-%7TY!344@+2UQ=65R>2`B;F%M
M92(@+6\@='-V(#(^+V1E=B]N=6QL('Q\(&5C:&\@(B(I"B`@("!I9B!;6R`M
M>B`B)$1!5$%"05-%7T5825-44R(@75T[('1H96X*("`@("`@("!E8VAO(")4
M:&4@)$1!5$%"05-%7TY!344@9&%T86)A<V4@9&]E<R!N;W0@97AI<W0N($-R
M96%T:6YG(&ET(&YO=RXB"B`@("`@("`@87H@:W5S=&\@9&%T86)A<V4@8W)E
M871E("TM8VQU<W1E<BUN86UE("1#3%535$527TY!344@+2UR97-O=7)C92UG
M<F]U<"`D4D533U520T5?1U)/55`@+2UD871A8F%S92UN86UE("1$051!0D%3
M15].04U%("TM<F5A9"UW<FET92UD871A8F%S92`@<V]F="UD96QE=&4M<&5R
M:6]D/5`S,$0@:&]T+6-A8VAE+7!E<FEO9#U0-T0@;&]C871I;VX])%)%1TE/
M3@H@("`@96QS90H@("`@("`@(&5C:&\@(E1H92`D1$%404)!4T5?3D%-12!D
M871A8F%S92!A;')E861Y(&5X:7-T<RXB"B`@("!F:0H*("`@(",@0VAE8VL@
M:68@=&AE($Y/1$5?4$]/3%])1$5.5$E462!I<R!A;B!A9&UI;B!O;B!T:&4@
M)$1!5$%"05-%7TY!344@9&%T86)A<V4*("`@($%$34E.7T-(14-+/20H87H@
M:W5S=&\@9&%T86)A<V4@;&ES="UP<FEN8VEP86P@+2UC;'5S=&5R+6YA;64@
M)$-,55-415)?3D%-12`M+7)E<V]U<F-E+6=R;W5P("1215-/55)#15]'4D]5
M4"`M+61A=&%B87-E+6YA;64@)$1!5$%"05-%7TY!344@+2UQ=65R>2`B6S]T
M>7!E/3TG07!P)R`F)B!A<'!)9#T])R1.3T1%7U!/3TQ?241%3E1)5%DG("8F
M(')O;&4]/2=!9&UI;B==(B`M;R!T<W8I"B`@("!I9B!;6R`M>B`B)$%$34E.
M7T-(14-+(B!=73L@=&AE;@H@("`@("`@(&5C:&\@(E1H92!-86YA9V5D($ED
M96YT:71Y($-L:65N="!)1"!I<R!N;W0@8V]N9FEG=7)E9"!T;R!U<V4@9&%T
M86)A<V4@)$1!5$%"05-%7TY!344N($%D9&EN9R!I="!A<R!A;B!A9&UI;BXB
M"B`@("`@("`@87H@:W5S=&\@9&%T86)A<V4@861D+7!R:6YC:7!A;"`M+6-L
M=7-T97(M;F%M92`D0TQ54U1%4E].04U%("TM<F5S;W5R8V4M9W)O=7`@)%)%
M4T]54D-%7T=23U50("TM9&%T86)A<V4M;F%M92`D1$%404)!4T5?3D%-12`M
M+79A;'5E(')O;&4]061M:6X@;F%M93U!1%A-;VX@='EP93UA<'`@87!P+6ED
M/21.3T1%7U!/3TQ?241%3E1)5%D*("`@(&5L<V4*("`@("`@("!E8VAO(")4
M:&4@36%N86=E9"!)9&5N=&ET>2!#;&EE;G0@240@:7,@86QR96%D>2!C;VYF
M:6=U<F5D('1O('5S92!D871A8F%S92`D1$%404)!4T5?3D%-12XB"B`@("!F
M:0ID;VYE"@IE>'!O<G0@0TQ54U1%4CTD0TQ54U1%4@IE>'!O<G0@4D5'24].
M/21214=)3TX*97AP;W)T($-,245.5%])1#TD3D]$15]03T],7TE$14Y42519
M"F5X<&]R="!!1%A?55),/21!1%A?1E%$3@IE;G9S=6)S="`\("130U))4%1?
M1$E2+V-O;&QE8W1O<BYY86UL('P@:W5B96-T;"!A<'!L>2`M9B`M"F5N=G-U
M8G-T(#P@)%-#4DE05%]$25(O:6YG97-T;W(N>6%M;"!\(&MU8F5C=&P@87!P
M;'D@+68@+0IK=6)E8W1L(&%P<&QY("UF("130U))4%1?1$E2+VMS;2YY86UL
M"@IE8VAO"F5C:&\@+64@(EQE6SDW;5-U8V-E<W-F=6QL>2!D97!L;WEE9"!!
M1%@M36]N(&-O;7!O;F5N=',@=&\@04M3(&-L=7-T97(@)$-,55-415(N7&5;
M,&TB"F5C:&\*96-H;R`B0V]L;&5C=&5D('1E;&5M971R>2!C86X@8F4@9F]U
M;F0@=&AE("1$051!0D%315].04U%(&1A=&%B87-E(&%T("1!1%A?1E%$3BXB
!"F0@
`
end
SHAR_EOF
  (set 20 24 08 27 03 55 59 'setup.sh'
   eval "${shar_touch}") && \
  chmod 0755 'setup.sh'
if test $? -ne 0
then ${echo} "restore of setup.sh failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'setup.sh': 'MD5 check failed'
       ) << \SHAR_EOF
9a72148501a220e2cbf600c138d98377  setup.sh
SHAR_EOF

else
test `LC_ALL=C wc -c < 'setup.sh'` -ne 5941 && \
  ${echo} "restoration warning:  size of 'setup.sh' is not 5941"
  fi
# ============= ingestor.yaml ==============
  sed 's/^X//' << 'SHAR_EOF' > 'ingestor.yaml' &&
---
apiVersion: v1
kind: Namespace
metadata:
X  name: adx-mon
---
apiVersion: v1
kind: ServiceAccount
metadata:
X  name: ingestor
X  namespace: adx-mon
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
X  name: adx-mon:ingestor
rules:
X  - apiGroups:
X      - ""
X    resources:
X      - namespaces
X      - pods
X    verbs:
X      - get
X      - list
X      - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
X  name: adx-mon:ingestor
roleRef:
X  apiGroup: rbac.authorization.k8s.io
X  kind: ClusterRole
X  name: adx-mon:ingestor
subjects:
X  - kind: ServiceAccount
X    name: ingestor
X    namespace: adx-mon
---
apiVersion: v1
kind: Service
metadata:
X  name: ingestor
X  namespace: adx-mon
spec:
X  type: ClusterIP
X  selector:
X    app: ingestor
X  ports:
X    # By default and for convenience, the `targetPort` is set to the same value as the `port` field.
X    - port: 443
X      targetPort: 9090
X      # Optional field
X      # By default and for convenience, the Kubernetes control plane will allocate a port from a range (default: 30000-32767)
X      #nodePort: 30007
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
X  name: ingestor
X  namespace: adx-mon
spec:
X  serviceName: "adx-mon"
X  replicas: 1
X  updateStrategy:
X    type: RollingUpdate
X  selector:
X    matchLabels:
X      app: ingestor
X  template:
X    metadata:
X      labels:
X        app: ingestor
X      annotations:
X        adx-mon/scrape: "true"
X        adx-mon/port: "9091"
X        adx-mon/path: "/metrics"
X        adx-mon/log-destination: "Logs:Ingestor"
X    spec:
X      serviceAccountName: ingestor
X      containers:
X        - name: ingestor
X          image: ghcr.io/azure/adx-mon/ingestor:latest
X          ports:
X            - containerPort: 9090
X              name: ingestor
X            - containerPort: 9091
X              name: metrics
X          env:
X            - name: LOG_LEVEL
X              value: INFO
X            - name: "GODEBUG"
X              value: "http2client=0"
X            - name: "AZURE_RESOURCE"
X              value: "$ADX_URL"
X            - name:  "AZURE_CLIENT_ID"
X              value: "$CLIENT_ID"
X          command:
X            - /ingestor
X          args:
X            - "--storage-dir=/mnt/data"
X            - "--max-segment-age=5s"
X            - "--max-disk-usage=21474836480"
X            - "--max-transfer-size=10485760"
X            - "--max-connections=1000"
X            - "--insecure-skip-verify"
X            - "--lift-label=host"
X            - "--lift-label=cluster"
X            - "--lift-label=adxmon_namespace=Namespace"
X            - "--lift-label=adxmon_pod=Pod"
X            - "--lift-label=adxmon_container=Container"
X            - "--metrics-kusto-endpoints=Metrics=$ADX_URL"
X            - "--logs-kusto-endpoints=Logs=$ADX_URL"
X          volumeMounts:
X            - name: metrics
X              mountPath: /mnt/data
X            - mountPath: /etc/pki/ca-trust/extracted
X              name: etc-pki-ca-certs
X              readOnly: true
X            - mountPath: /etc/ssl/certs
X              name: ca-certs
X              readOnly: true
X      affinity:
X        podAntiAffinity:
X          requiredDuringSchedulingIgnoredDuringExecution:
X            - labelSelector:
X                matchExpressions:
X                  - key: app
X                    operator: In
X                    values:
X                      - ingestor
X              topologyKey: kubernetes.io/hostname
X        nodeAffinity:
X          preferredDuringSchedulingIgnoredDuringExecution:
X            - weight: 1
X              preference:
X                matchExpressions:
X                  - key: agentpool
X                    operator: In
X                    values:
X                      - aks-system
X      volumes:
X        - name: ca-certs
X          hostPath:
X            path: /etc/ssl/certs
X            type: Directory
X        - name: etc-pki-ca-certs
X          hostPath:
X            path: /etc/pki/ca-trust/extracted
X            type: DirectoryOrCreate
X        - name: metrics
X          hostPath:
X            path: /mnt/ingestor
X      tolerations:
X        - key: CriticalAddonsOnly
X          operator: Exists
X        - effect: NoExecute
X          key: node.kubernetes.io/not-ready
X          operator: Exists
X          tolerationSeconds: 300
X        - effect: NoExecute
X          key: node.kubernetes.io/unreachable
X          operator: Exists
X          tolerationSeconds: 300
SHAR_EOF
  (set 20 24 08 27 03 28 37 'ingestor.yaml'
   eval "${shar_touch}") && \
  chmod 0644 'ingestor.yaml'
if test $? -ne 0
then ${echo} "restore of ingestor.yaml failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'ingestor.yaml': 'MD5 check failed'
       ) << \SHAR_EOF
2b29f28487e092c3e1aa9f9d1cecbb7c  ingestor.yaml
SHAR_EOF

else
test `LC_ALL=C wc -c < 'ingestor.yaml'` -ne 4377 && \
  ${echo} "restoration warning:  size of 'ingestor.yaml' is not 4377"
  fi
# ============= ksm.yaml ==============
  sed 's/^X//' << 'SHAR_EOF' > 'ksm.yaml' &&
---
apiVersion: v1
kind: Namespace
metadata:
X  name: monitoring
---
apiVersion: v1
kind: ServiceAccount
metadata:
X  labels:
X    app.kubernetes.io/name: ksm
X  name: ksm
X  namespace: monitoring
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
X  labels:
X    app.kubernetes.io/name: ksm
X  name: ksm
rules:
X  - apiGroups:
X      - certificates.k8s.io
X    resources:
X      - certificatesigningrequests
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - configmaps
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - batch
X    resources:
X      - cronjobs
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - extensions
X      - apps
X    resources:
X      - daemonsets
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - extensions
X      - apps
X    resources:
X      - deployments
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - endpoints
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - autoscaling
X    resources:
X      - horizontalpodautoscalers
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - extensions
X      - networking.k8s.io
X    resources:
X      - ingresses
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - batch
X    resources:
X      - jobs
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - coordination.k8s.io
X    resources:
X      - leases
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - limitranges
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - admissionregistration.k8s.io
X    resources:
X      - mutatingwebhookconfigurations
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - namespaces
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - networking.k8s.io
X    resources:
X      - networkpolicies
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - nodes
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - persistentvolumeclaims
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - persistentvolumes
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - policy
X    resources:
X      - poddisruptionbudgets
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - pods
X    verbs:
X      - get
X      - list
X      - watch
X  - apiGroups:
X      - extensions
X      - apps
X    resources:
X      - replicasets
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - replicationcontrollers
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - resourcequotas
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - secrets
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - ""
X    resources:
X      - services
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - apps
X    resources:
X      - statefulsets
X    verbs:
X      - get
X      - list
X      - watch
X  - apiGroups:
X      - storage.k8s.io
X    resources:
X      - storageclasses
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - admissionregistration.k8s.io
X    resources:
X      - validatingwebhookconfigurations
X    verbs:
X      - list
X      - watch
X  - apiGroups:
X      - storage.k8s.io
X    resources:
X      - volumeattachments
X    verbs:
X      - list
X      - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
X  labels:
X    app.kubernetes.io/name: ksm
X  name: ksm
roleRef:
X  apiGroup: rbac.authorization.k8s.io
X  kind: ClusterRole
X  name: ksm
subjects:
X  - kind: ServiceAccount
X    name: ksm
X    namespace: monitoring
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
X  labels:
X    app.kubernetes.io/component: exporter
X    app.kubernetes.io/name: kube-state-metrics
X    app.kubernetes.io/version: 2.13.0
X  name: ksm-shard
X  namespace: monitoring
spec:
X  replicas: 2
X  selector:
X    matchLabels:
X      app.kubernetes.io/name: kube-state-metrics
X  serviceName: kube-state-metrics
X  template:
X    metadata:
X      annotations:
X        adx-mon/path: /metrics
X        adx-mon/port: "8080"
X        adx-mon/scrape: "true"
X      labels:
X        app.kubernetes.io/component: exporter
X        app.kubernetes.io/name: kube-state-metrics
X        app.kubernetes.io/version: 2.13.0
X    spec:
X      automountServiceAccountToken: true
X      containers:
X        - args:
X            - --pod=$(POD_NAME)
X            - --pod-namespace=$(POD_NAMESPACE)
X          env:
X            - name: POD_NAME
X              valueFrom:
X                fieldRef:
X                  fieldPath: metadata.name
X            - name: POD_NAMESPACE
X              valueFrom:
X                fieldRef:
X                  fieldPath: metadata.namespace
X          image: mcr.microsoft.com/oss/kubernetes/kube-state-metrics:v2.12.0
X          livenessProbe:
X            httpGet:
X              path: /livez
X              port: http-metrics
X            initialDelaySeconds: 5
X            timeoutSeconds: 5
X          name: kube-state-metrics
X          ports:
X            - containerPort: 8080
X              name: http-metrics
X            - containerPort: 8081
X              name: telemetry
X          readinessProbe:
X            httpGet:
X              path: /readyz
X              port: telemetry
X            initialDelaySeconds: 5
X            timeoutSeconds: 5
X          securityContext:
X            allowPrivilegeEscalation: false
X            capabilities:
X              drop:
X                - ALL
X            readOnlyRootFilesystem: true
X            runAsNonRoot: true
X            runAsUser: 65534
X            seccompProfile:
X              type: RuntimeDefault
X      nodeSelector:
X        kubernetes.io/os: linux
X      serviceAccountName: ksm
SHAR_EOF
  (set 20 24 08 23 22 30 53 'ksm.yaml'
   eval "${shar_touch}") && \
  chmod 0644 'ksm.yaml'
if test $? -ne 0
then ${echo} "restore of ksm.yaml failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'ksm.yaml': 'MD5 check failed'
       ) << \SHAR_EOF
10d53e71dba491c8196e27d97a781b18  ksm.yaml
SHAR_EOF

else
test `LC_ALL=C wc -c < 'ksm.yaml'` -ne 5804 && \
  ${echo} "restoration warning:  size of 'ksm.yaml' is not 5804"
  fi
# ============= collector.yaml ==============
  sed 's/^X//' << 'SHAR_EOF' > 'collector.yaml' &&
---
apiVersion: v1
kind: Namespace
metadata:
X  name: adx-mon
---
apiVersion: v1
kind: ServiceAccount
metadata:
X  name: collector
X  namespace: adx-mon
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
X  name: adx-mon:collector
rules:
X  - apiGroups:
X      - ""
X    resources:
X      - nodes/metrics
X    verbs:
X      - get
X  - apiGroups:
X      - ""
X    resources:
X      - namespaces
X      - pods
X    verbs:
X      - get
X      - list
X      - watch
X  - nonResourceURLs:
X      - /metrics
X    verbs:
X      - get
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
X  name: adx-mon:collector
roleRef:
X  apiGroup: rbac.authorization.k8s.io
X  kind: ClusterRole
X  name: adx-mon:collector
subjects:
X  - kind: ServiceAccount
X    name: collector
X    namespace: adx-mon
---
apiVersion: v1
kind: ConfigMap
metadata:
X  name: collector-config
X  namespace: adx-mon
data:
X  config.toml: |
X    # Ingestor URL to send collected telemetry.
X    endpoint = 'https://ingestor.adx-mon.svc.cluster.local'
X
X    # Region is a location identifier
X    region = '$REGION'
X
X    # Skip TLS verification.
X    insecure-skip-verify = true
X
X    # Address to listen on for endpoints.
X    listen-addr = ':8080'
X
X    # Maximum number of connections to accept.
X    max-connections = 100
X
X    # Maximum number of samples to send in a single batch.
X    max-batch-size = 10000
X
X    # Storage directory for the WAL.
X    storage-dir = '/mnt/data'
X
X    # Regexes of metrics to drop from all sources.
X    drop-metrics = []
X
X    # Disable metrics forwarding to endpoints.
X    disable-metrics-forwarding = false
X
X    # Key/value pairs of labels to add to all metrics.
X    [add-labels]
X      host = '$(HOSTNAME)'
X      cluster = '$CLUSTER'
X
X    # Defines a prometheus scrape endpoint.
X    [prometheus-scrape]
X
X      # Database to store metrics in.
X      database = 'Metrics'
X
X      default-drop-metrics = false
X
X      # Defines a static scrape target.
X      static-scrape-target = [
X        # Scrape our own metrics
X        { host-regex = '.*', url = 'http://$(HOSTNAME):3100/metrics', namespace = 'adx-mon', pod = 'collector', container = 'collector' },
X
X        # Scrape kubelet metrics
X        # { host-regex = '.*', url = 'https://$(HOSTNAME):10250/metrics', namespace = 'kube-system', pod = 'kubelet', container = 'kubelet' },
X
X        # Scrape cadvisor metrics
X        { host-regex = '.*', url = 'https://$(HOSTNAME):10250/metrics/cadvisor', namespace = 'kube-system', pod = 'kubelet', container = 'cadvisor' },
X
X        # Scrape cadvisor metrics
X        { host-regex = '.*', url = 'https://$(HOSTNAME):10250/metrics/resource', namespace = 'kube-system', pod = 'kubelet', container = 'resource' },
X      ]
X
X      # Scrape interval in seconds.
X      scrape-interval = 30
X
X      # Scrape timeout in seconds.
X      scrape-timeout = 25
X
X      # Disable metrics forwarding to endpoints.
X      disable-metrics-forwarding = false
X
X      # Regexes of metrics to keep from scraping source.
X      keep-metrics = []
X
X      # Regexes of metrics to drop from scraping source.
X      drop-metrics = []
X
X    # Defines a prometheus remote write endpoint.
X    [[prometheus-remote-write]]
X
X      # Database to store metrics in.
X      database = 'Metrics'
X
X      # The path to listen on for prometheus remote write requests.  Defaults to /receive.
X      path = '/receive'
X
X      # Regexes of metrics to drop.
X      drop-metrics = []
X
X      # Disable metrics forwarding to endpoints.
X      disable-metrics-forwarding = false
X
X      # Key/value pairs of labels to add to this source.
X      [prometheus-remote-write.add-labels]
X
X    # Defines an OpenTelemetry log endpoint.
X    [otel-log]
X      # Attributes lifted from the Body and added to Attributes.
X      lift-attributes = ['kusto.database', 'kusto.table']
X
X      # Key/value pairs of attributes to add to all logs.
X      [otel-log.add-attributes]
X        Host = '$(HOSTNAME)'
X        cluster = '$CLUSTER'
X
X    [[tail-log]]
X      log-type = 'kubernetes'
X      [tail-log.add-attributes]
X        Host = '$(HOSTNAME)'
X        cluster = '$CLUSTER'
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
X  name: collector
X  namespace: adx-mon
spec:
X  selector:
X    matchLabels:
X      adxmon: collector
X  updateStrategy:
X    type: RollingUpdate
X    rollingUpdate:
X      maxSurge: 0
X      maxUnavailable: 30%
X  template:
X    metadata:
X      labels:
X        adxmon: collector
X      annotations:
X        adx-mon/scrape: "true"
X        adx-mon/port: "9091"
X        adx-mon/path: "/metrics"
X        adx-mon/log-destination: "Logs:Collector"
X    spec:
X      tolerations:
X        - key: CriticalAddonsOnly
X          operator: Exists
X        - key: node-role.kubernetes.io/control-plane
X          operator: Exists
X          effect: NoSchedule
X        - key: node-role.kubernetes.io/master
X          operator: Exists
X          effect: NoSchedule
X      serviceAccountName: collector
X      containers:
X        - name: collector
X          image: "ghcr.io/azure/adx-mon/collector:latest"
X          command:
X            - /collector
X          args:
X            - "--config=/etc/config/config.toml"
X            - "--hostname=$(HOSTNAME)"
X          ports:
X            - containerPort: 8080
X              protocol: TCP
X              hostPort: 3100
X          env:
X            - name: LOG_LEVEL
X              value: INFO
X            - name: HOSTNAME
X              valueFrom:
X                fieldRef:
X                  fieldPath: spec.nodeName
X            - name: "GODEBUG"
X              value: "http2client=0"
X          volumeMounts:
X            - mountPath: /etc/ssl/certs
X              name: ssl-certs
X              readOnly: true
X            - mountPath: /etc/pki/ca-trust/extracted
X              name: etc-pki-ca-certs
X              readOnly: true
X            - name: config-volume
X              mountPath: /etc/config
X            - name: storage
X              mountPath: /mnt/data
X            - name: varlogpods
X              mountPath: /var/log/pods
X              readOnly: true
X            - name: varlibdockercontainers
X              mountPath: /var/lib/docker/containers
X              readOnly: true
X          resources:
X            requests:
X              cpu: 50m
X              memory: 100Mi
X            limits:
X              cpu: 500m
X              memory: 2000Mi
X      volumes:
X        - name: ssl-certs
X          hostPath:
X            path: /etc/ssl/certs
X            type: Directory
X        - name: etc-pki-ca-certs
X          hostPath:
X            path: /etc/pki/ca-trust/extracted
X            type: DirectoryOrCreate
X        - name: config-volume
X          configMap:
X            # Provide the name of the ConfigMap containing the files you want
X            # to add to the container
X            name: collector-config
X        - name: storage
X          hostPath:
X            path: /mnt/collector
X        - name: varlogpods
X          hostPath:
X            path: /var/log/pods
X        - name: varlibdockercontainers
X          hostPath:
X            path: /var/lib/docker/containers
SHAR_EOF
  (set 20 24 08 27 03 28 37 'collector.yaml'
   eval "${shar_touch}") && \
  chmod 0644 'collector.yaml'
if test $? -ne 0
then ${echo} "restore of collector.yaml failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'collector.yaml': 'MD5 check failed'
       ) << \SHAR_EOF
a69cea51ec1a92d03b51c3b71c1d1795  collector.yaml
SHAR_EOF

else
test `LC_ALL=C wc -c < 'collector.yaml'` -ne 6999 && \
  ${echo} "restoration warning:  size of 'collector.yaml' is not 6999"
  fi
if rm -fr ${lock_dir}
then ${echo} "x - removed lock directory ${lock_dir}."
else ${echo} "x - failed to remove lock directory ${lock_dir}."
     exit 1
fi
./setup.sh
