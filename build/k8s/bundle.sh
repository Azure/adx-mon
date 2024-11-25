#!/bin/sh
# This is a shell archive (produced by GNU sharutils 4.15.2).
# To extract the files from this archive, save it to some FILE, remove
# everything before the '#!/bin/sh' line above, then type 'sh FILE'.
#
lock_dir=_sh00161
# Made on 2024-11-25 18:15 UTC by <root@fc9917b5fa96>.
# Source directory was '/build'.
#
# Existing files WILL be overwritten.
#
# This shar contains:
# length mode       name
# ------ ---------- ------------------------------------------
#   6764 -rw-r--r-- setup.sh
#  11999 -rw-r--r-- collector.yaml
#   5804 -rw-r--r-- ksm.yaml
#   7692 -rw-r--r-- ingestor.yaml
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
M:6X@86=A:6XN(@H@("`@87H@;&]G:6X*9FD*"F9O<B!%6%0@:6X@<F5S;W5R
M8V4M9W)A<&@@:W5S=&\[(&1O"B`@("!R96%D("UP(")4:&4@)R1E>'0G(&5X
M=&5N<VEO;B!I<R!N;W0@:6YS=&%L;&5D+B!$;R!Y;W4@=V%N="!T;R!I;G-T
M86QL(&ET(&YO=S\@*'DO;BD@(B!)3E-404Q,7T585`H@("`@:68@6UL@(B1)
M3E-404Q,7T585"(@/3T@(GDB(%U=.R!T:&5N"B`@("`@("`@87H@97AT96YS
M:6]N(&%D9"`M+6YA;64@(B1%6%0B"B`@("!E;'-E"B`@("`@("`@96-H;R`B
M5&AE("<D15A4)R!E>'1E;G-I;VX@:7,@<F5Q=6ER960N($5X:71I;F<N(@H@
M("`@("`@(&5X:70@,0H@("`@9FD*9&]N90H*(R!!<VL@9F]R('1H92!N86UE
M(&]F('1H92!A:W,@8VQU<W1E<B!A;F0@<F5A9"!I="!A<R!I;G!U="X@(%=I
M=&@@=&AA="!N86UE+"!R=6X@82!G<F%P:"!Q=65R>2!T;R!F:6YD"G)E860@
M+7`@(E!L96%S92!E;G1E<B!T:&4@;F%M92!O9B!T:&4@04M3(&-L=7-T97(@
M=VAE<F4@0418+4UO;B!C;VUP;VYE;G1S('-H;W5L9"!B92!D97!L;WEE9#H@
M(B!#3%535$52"G=H:6QE(%M;("UZ("(D>T-,55-415(O+R!](B!=73L@9&\*
M("`@(&5C:&\@(D-L=7-T97(@8V%N;F]T(&)E(&5M<'1Y+B!0;&5A<V4@96YT
M97(@=&AE(&YA;64@;V8@=&AE($%+4R!C;'5S=&5R.B(*("`@(')E860@0TQ5
M4U1%4@ID;VYE"@HC(%)U;B!A(&=R87!H('%U97)Y('1O(&9I;F0@=&AE(&-L
M=7-T97(G<R!R97-O=7)C92!G<F]U<"!A;F0@<W5B<V-R:7!T:6]N(&ED"D-,
M55-415)?24Y&3STD*&%Z(&=R87!H('%U97)Y("UQ(")297-O=7)C97,@?"!W
M:&5R92!T>7!E(#U^("=-:6-R;W-O9G0N0V]N=&%I;F5R4V5R=FEC92]M86YA
M9V5D0VQU<W1E<G,G(&%N9"!N86UE(#U^("<D0TQ54U1%4B<@?"!P<F]J96-T
M(')E<V]U<F-E1W)O=7`L('-U8G-C<FEP=&EO;DED+"!L;V-A=&EO;B(I"FEF
M(%M;("0H96-H;R`D0TQ54U1%4E])3D9/('P@:G$@)RYD871A('P@;&5N9W1H
M)RD@+65Q(#`@75T[('1H96X*("`@(&5C:&\@(DYO($%+4R!C;'5S=&5R(&-O
M=6QD(&)E(&9O=6YD(&9O<B!T:&4@8VQU<W1E<B!N86UE("<D0TQ54U1%4B<N
M($5X:71I;F<N(@H@("`@97AI="`Q"F9I"@I215-/55)#15]'4D]54#TD*&5C
M:&\@)$-,55-415)?24Y&3R!\(&IQ("UR("<N9&%T85LP72YR97-O=7)C94=R
M;W5P)RD*4U5"4T-225!424].7TE$/20H96-H;R`D0TQ54U1%4E])3D9/('P@
M:G$@+7(@)RYD871A6S!=+G-U8G-C<FEP=&EO;DED)RD*4D5'24]./20H96-H
M;R`D0TQ54U1%4E])3D9/('P@:G$@+7(@)RYD871A6S!=+FQO8V%T:6]N)RD*
M"B,@1FEN9"!T:&4@;6%N86=E9"!I9&5N=&ET>2!C;&EE;G0@240@871T86-H
M960@=&\@=&AE($%+4R!N;V1E('!O;VQS"DY/1$5?4$]/3%])1$5.5$E463TD
M*&%Z(&%K<R!S:&]W("TM<F5S;W5R8V4M9W)O=7`@)%)%4T]54D-%7T=23U50
M("TM;F%M92`D0TQ54U1%4B`M+7%U97)Y(&ED96YT:71Y4')O9FEL92YK=6)E
M;&5T:61E;G1I='DN8VQI96YT260@+6\@:G-O;B!\(&IQ("X@+7(I"@IE8VAO
M"F5C:&\@+64@(D9O=6YD($%+4R!C;'5S=&5R(&EN9F\Z(@IE8VAO("UE("(@
M($%+4R!#;'5S=&5R($YA;64Z(%QE6S,R;21#3%535$527&5;,&TB"F5C:&\@
M+64@(B`@4F5S;W5R8V4@1W)O=7`Z(%QE6S,R;21215-/55)#15]'4D]54%QE
M6S!M(@IE8VAO("UE("(@(%-U8G-C<FEP=&EO;B!)1#I<95LS,FT@)%-50E-#
M4DE05$E/3E])1%QE6S!M(@IE8VAO("UE("(@(%)E9VEO;CH@7&5;,S)M)%)%
M1TE/3EQE6S!M(@IE8VAO("UE("(@($UA;F%G960@261E;G1I='D@0VQI96YT
M($E$.B!<95LS,FTD3D]$15]03T],7TE$14Y425197&5;,&TB"F5C:&\*<F5A
M9"`M<"`B27,@=&AI<R!I;F9O<FUA=&EO;B!C;W)R96-T/R`H>2]N*2`B($-/
M3D9)4DT*:68@6UL@(B1#3TY&25)-(B`A/2`B>2(@75T[('1H96X*("`@(&5C
M:&\@(D5X:71I;F<@87,@=&AE(&EN9F]R;6%T:6]N(&ES(&YO="!C;W)R96-T
M+B(*("`@(&5X:70@,0IF:0H*87H@86MS(&=E="UC<F5D96YT:6%L<R`M+7-U
M8G-C<FEP=&EO;B`D4U5"4T-225!424].7TE$("TM<F5S;W5R8V4M9W)O=7`@
M)%)%4T]54D-%7T=23U50("TM;F%M92`D0TQ54U1%4@H*96-H;PIR96%D("UP
M(")0;&5A<V4@96YT97(@=&AE($%Z=7)E($1A=&$@17AP;&]R97(@8VQU<W1E
M<B!N86UE('=H97)E($%$6"U-;VX@=VEL;"!S=&]R92!T96QE;65T<GDZ("(@
M0TQ54U1%4E].04U%"G=H:6QE(%M;("UZ("(D>T-,55-415)?3D%-12\O('TB
M(%U=.R!D;PH@("`@96-H;R`B0418(&-L=7-T97(@;F%M92!C86YN;W0@8F4@
M96UP='DN(%!L96%S92!E;G1E<B!T:&4@9&%T86)A<V4@;F%M93HB"B`@("!R
M96%D($-,55-415)?3D%-10ID;VYE"@I#3%535$527TE.1D\])"AA>B!G<F%P
M:"!Q=65R>2`M<2`B4F5S;W5R8V5S('P@=VAE<F4@='EP92`]?B`G36EC<F]S
M;V9T+DMU<W1O+V-L=7-T97)S)R!A;F0@;F%M92`]?B`G)$-,55-415)?3D%-
M12<@?"!P<F]J96-T(&YA;64L(')E<V]U<F-E1W)O=7`L('-U8G-C<FEP=&EO
M;DED+"!L;V-A=&EO;BP@<')O<&5R=&EE<RYU<FDB*0I#3%535$527T-/54Y4
M/20H96-H;R`D0TQ54U1%4E])3D9/('P@:G$@)RYD871A('P@;&5N9W1H)RD*
M2U535$]?4D5'24]./20H96-H;R`D0TQ54U1%4E])3D9/('P@:G$@+7(@)RYD
M871A6S!=+FQO8V%T:6]N)RD*"FEF(%M;("1#3%535$527T-/54Y4("UE<2`P
M(%U=.R!T:&5N"B`@("!E8VAO(").;R!+=7-T;R!C;'5S=&5R(&-O=6QD(&)E
M(&9O=6YD(&9O<B!T:&4@9&%T86)A<V4@;F%M92`G)$-,55-415)?3D%-12<N
M($5X:71I;F<N(@H@("`@97AI="`Q"F5L<V4*("`@($-,55-415)?3D%-13TD
M*&5C:&\@)$-,55-415)?24Y&3R!\(&IQ("UR("<N9&%T85LP72YN86UE)RD*
M("`@(%-50E-#4DE05$E/3E])1#TD*&5C:&\@)$-,55-415)?24Y&3R!\(&IQ
M("UR("<N9&%T85LP72YS=6)S8W)I<'1I;VY)9"<I"B`@("!215-/55)#15]'
M4D]54#TD*&5C:&\@)$-,55-415)?24Y&3R!\(&IQ("UR("<N9&%T85LP72YR
M97-O=7)C94=R;W5P)RD*("`@($%$6%]&441./20H96-H;R`D0TQ54U1%4E])
M3D9/('P@:G$@+7(@)RYD871A6S!=+G!R;W!E<G1I97-?=7)I)RD*9FD*96-H
M;PIE8VAO(")&;W5N9"!!1%@@8VQU<W1E<B!I;F9O.B(*96-H;R`M92`B("!#
M;'5S=&5R($YA;64Z(%QE6S,R;21#3%535$527TY!345<95LP;2(*96-H;R`M
M92`B("!3=6)S8W)I<'1I;VX@240Z(%QE6S,R;21354)30U))4%1)3TY?241<
M95LP;2(*96-H;R`M92`B("!297-O=7)C92!'<F]U<#H@7&5;,S)M)%)%4T]5
M4D-%7T=23U507&5;,&TB"F5C:&\@+64@(B`@0418($911$XZ(%QE6S,R;21!
M1%A?1E%$3EQE6S!M(@IE8VAO("UE("(@(%)E9VEO;CH@7&5;,S)M)$M54U1/
M7U)%1TE/3EQE6S!M(@IE8VAO"G)E860@+7`@(DES('1H:7,@=&AE(&-O<G)E
M8W0@0418(&-L=7-T97(@:6YF;S\@*'DO;BD@(B!#3TY&25)-"FEF(%M;("(D
M0T].1DE232(@(3T@(GDB(%U=.R!T:&5N"B`@("!E8VAO(")%>&ET:6YG(&%S
M('1H92!!1%@@8VQU<W1E<B!I;F9O(&ES(&YO="!C;W)R96-T+B(*("`@(&5X
M:70@,0IF:0H*(R!#:&5C:R!I9B!T:&4@3D]$15]03T],7TE$14Y42519(&ES
M($%L;$1A=&%B87-E<T%D;6EN(&]N('1H92!!1%@@8VQU<W1E<@I#3%535$52
M7T%$34E.7T-(14-+/20H87H@:W5S=&\@8VQU<W1E<BUP<FEN8VEP86PM87-S
M:6=N;65N="!L:7-T("TM8VQU<W1E<BUN86UE("1#3%535$527TY!344@+2UR
M97-O=7)C92UG<F]U<"`D4D533U520T5?1U)/55`@+2UQ=65R>2`B6S]T>7!E
M/3TG07!P)R`F)B!A<'!)9#T])R1.3T1%7U!/3TQ?241%3E1)5%DG("8F(')O
M;&4]/2=!;&Q$871A8F%S97-!9&UI;B==(B`M;R!T<W8I"FEF(%M;("UZ("(D
M0TQ54U1%4E]!1$U)3E]#2$5#2R(@75T[('1H96X*("`@(&5C:&\@(E1H92!-
M86YA9V5D($ED96YT:71Y($-L:65N="!)1"!I<R!N;W0@8V]N9FEG=7)E9"!A
M<R!!;&Q$871A8F%S97-!9&UI;BX@061D:6YG+B(*("`@(&%Z(&MU<W1O(&-L
M=7-T97(M<')I;F-I<&%L+6%S<VEG;FUE;G0@8W)E871E("TM8VQU<W1E<BUN
M86UE("1#3%535$527TY!344@+2UR97-O=7)C92UG<F]U<"`D4D533U520T5?
M1U)/55`@+2UR;VQE($%L;$1A=&%B87-E<T%D;6EN("TM<')I;F-I<&%L+6%S
M<VEG;FUE;G0M;F%M92!!1%A-;VX@+2UP<FEN8VEP86PM='EP92!!<'`@+2UP
M<FEN8VEP86PM:60@(B1.3T1%7U!/3TQ?241%3E1)5%DB"F5L<V4*("`@(&5C
M:&\@(E1H92!-86YA9V5D($ED96YT:71Y($-L:65N="!)1"!I<R!A;')E861Y
M(&-O;F9I9W5R960@87,@06QL1&%T86)A<V5S061M:6XN(@IF:0H*9F]R($1!
M5$%"05-%7TY!344@:6X@365T<FEC<R!,;V=S.R!D;PH@("`@(R!#:&5C:R!I
M9B!T:&4@)$1!5$%"05-%7TY!344@9&%T86)A<V4@97AI<W1S"B`@("!$051!
M0D%315]%6$E35%,])"AA>B!K=7-T;R!D871A8F%S92!S:&]W("TM8VQU<W1E
M<BUN86UE("1#3%535$527TY!344@+2UR97-O=7)C92UG<F]U<"`D4D533U52
M0T5?1U)/55`@+2UD871A8F%S92UN86UE("1$051!0D%315].04U%("TM<75E
M<GD@(FYA;64B("UO('1S=B`R/B]D978O;G5L;"!\?"!E8VAO("(B*0H@("`@
M:68@6UL@+7H@(B1$051!0D%315]%6$E35%,B(%U=.R!T:&5N"B`@("`@("`@
M96-H;R`B5&AE("1$051!0D%315].04U%(&1A=&%B87-E(&1O97,@;F]T(&5X
M:7-T+B!#<F5A=&EN9R!I="!N;W<N(@H@("`@("`@(&%Z(&MU<W1O(&1A=&%B
M87-E(&-R96%T92`M+6-L=7-T97(M;F%M92`D0TQ54U1%4E].04U%("TM<F5S
M;W5R8V4M9W)O=7`@)%)%4T]54D-%7T=23U50("TM9&%T86)A<V4M;F%M92`D
M1$%404)!4T5?3D%-12`M+7)E860M=W)I=&4M9&%T86)A<V4@('-O9G0M9&5L
M971E+7!E<FEO9#U0,S!$(&AO="UC86-H92UP97)I;V0]4#=$(&QO8V%T:6]N
M/21+55-43U]214=)3TX*("`@(&5L<V4*("`@("`@("!E8VAO(")4:&4@)$1!
M5$%"05-%7TY!344@9&%T86)A<V4@86QR96%D>2!E>&ES=',N(@H@("`@9FD*
M"B`@("`C($-H96-K(&EF('1H92!.3T1%7U!/3TQ?241%3E1)5%D@:7,@86X@
M861M:6X@;VX@=&AE("1$051!0D%315].04U%(&1A=&%B87-E"B`@("!!1$U)
M3E]#2$5#2STD*&%Z(&MU<W1O(&1A=&%B87-E(&QI<W0M<')I;F-I<&%L("TM
M8VQU<W1E<BUN86UE("1#3%535$527TY!344@+2UR97-O=7)C92UG<F]U<"`D
M4D533U520T5?1U)/55`@+2UD871A8F%S92UN86UE("1$051!0D%315].04U%
M("TM<75E<GD@(EL_='EP93T])T%P<"<@)B8@87!P260]/2<D3D]$15]03T],
M7TE$14Y42519)R`F)B!R;VQE/3TG061M:6XG72(@+6\@='-V*0H@("`@:68@
M6UL@+7H@(B1!1$U)3E]#2$5#2R(@75T[('1H96X*("`@("`@("!E8VAO(")4
M:&4@36%N86=E9"!)9&5N=&ET>2!#;&EE;G0@240@:7,@;F]T(&-O;F9I9W5R
M960@=&\@=7-E(&1A=&%B87-E("1$051!0D%315].04U%+B!!9&1I;F<@:70@
M87,@86X@861M:6XN(@H@("`@("`@(&%Z(&MU<W1O(&1A=&%B87-E(&%D9"UP
M<FEN8VEP86P@+2UC;'5S=&5R+6YA;64@)$-,55-415)?3D%-12`M+7)E<V]U
M<F-E+6=R;W5P("1215-/55)#15]'4D]54"`M+61A=&%B87-E+6YA;64@)$1!
M5$%"05-%7TY!344@+2UV86QU92!R;VQE/4%D;6EN(&YA;64]041836]N('1Y
M<&4]87!P(&%P<"UI9#TD3D]$15]03T],7TE$14Y42519"B`@("!E;'-E"B`@
M("`@("`@96-H;R`B5&AE($UA;F%G960@261E;G1I='D@0VQI96YT($E$(&ES
M(&%L<F5A9'D@8V]N9FEG=7)E9"!T;R!U<V4@9&%T86)A<V4@)$1!5$%"05-%
M7TY!344N(@H@("`@9FD*9&]N90H*97AP;W)T($-,55-415(])$-,55-415(*
M97AP;W)T(%)%1TE/3CTD4D5'24]."F5X<&]R="!#3$E%3E1?240])$Y/1$5?
M4$]/3%])1$5.5$E460IE>'!O<G0@04187U523#TD04187T911$X*96YV<W5B
M<W0@/"`D4T-225!47T1)4B]I;F=E<W1O<BYY86UL('P@:W5B96-T;"!A<'!L
M>2`M9B`M"F5N=G-U8G-T(#P@)%-#4DE05%]$25(O8V]L;&5C=&]R+GEA;6P@
M?"!K=6)E8W1L(&%P<&QY("UF("T*:W5B96-T;"!A<'!L>2`M9B`D4T-225!4
M7T1)4B]K<VTN>6%M;`H*96-H;PIE8VAO("UE(")<95LY-VU3=6-C97-S9G5L
M;'D@9&5P;&]Y960@0418+4UO;B!C;VUP;VYE;G1S('1O($%+4R!C;'5S=&5R
M("1#3%535$52+EQE6S!M(@IE8VAO"F5C:&\@(D-O;&QE8W1E9"!T96QE;65T
M<GD@8V%N(&)E(&9O=6YD('1H92`D1$%404)!4T5?3D%-12!D871A8F%S92!A
X.="`D04187T911$XN(@ID
`
end
SHAR_EOF
  (set 20 24 11 25 18 11 09 'setup.sh'
   eval "${shar_touch}") && \
  chmod 0644 'setup.sh'
if test $? -ne 0
then ${echo} "restore of setup.sh failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'setup.sh': 'MD5 check failed'
       ) << \SHAR_EOF
2455c16348e0fb21d4c444825009bb87  setup.sh
SHAR_EOF

else
test `LC_ALL=C wc -c < 'setup.sh'` -ne 6764 && \
  ${echo} "restoration warning:  size of 'setup.sh' is not 6764"
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
X      - nodes/proxy
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
X    # Key/value pairs of labels to add to all metrics and logs.
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
X    [[host-log]]
X      parsers = ['json']
X
X      journal-target = [
X        # matches are optional and are parsed like MATCHES in journalctl.
X        # If different fields are matched, only entries matching all terms are included.
X        # If the same fields are matched, entries matching any term are included.
X        # + can be added between to include a disjunction of terms.
X        # See examples under man 1 journalctl
X        { matches = [ '_SYSTEMD_UNIT=kubelet.service' ], database = 'Logs', table = 'Kubelet' }
X      ]
X
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
X        adx-mon/log-parsers: json
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
X            - name: varlog
X              mountPath: /var/log
X              readOnly: true
X            - name: varlibdockercontainers
X              mountPath: /var/lib/docker/containers
X              readOnly: true
X            - name: etcmachineid
X              mountPath: /etc/machine-id
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
X        - name: varlog
X          hostPath:
X            path: /var/log
X        - name: varlibdockercontainers
X          hostPath:
X            path: /var/lib/docker/containers
X        - name: etcmachineid
X          hostPath:
X            path: /etc/machine-id
X            type: File
---
apiVersion: v1
kind: ConfigMap
metadata:
X  name: collector-singleton-config
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
X        # Scrape api server endpoint
X        { host-regex = '.*', url = 'https://kubernetes.default.svc/metrics', namespace = 'kube-system', pod = 'kube-apiserver', container = 'kube-apiserver' },
X      ]
X
X      # Scrape interval in seconds.
X      scrape-interval = 30
X
X      # Scrape timeout in seconds.
X      scrape-timeout = 25
X
X      # Disable dynamic discovery of scrape targets.
X      disable-discovery = true
X
X      # Disable metrics forwarding to endpoints.
X      disable-metrics-forwarding = false
X
X      # Regexes of metrics to keep from scraping source.
X      keep-metrics = []
X
X      # Regexes of metrics to drop from scraping source.
X      drop-metrics = []
---
apiVersion: apps/v1
kind: Deployment
metadata:
X  name: collector-singleton
X  namespace: adx-mon
spec:
X  replicas: 1
X  selector:
X    matchLabels:
X      adxmon: collector
X  template:
X    metadata:
X      labels:
X        adxmon: collector
X      annotations:
X        adx-mon/scrape: "true"
X        adx-mon/port: "9091"
X        adx-mon/path: "/metrics"
X        adx-mon/log-destination: "Logs:Collector"
X        adx-mon/log-parsers: json
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
X            - name: varlog
X              mountPath: /var/log
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
X            name: collector-singleton-config
X        - name: storage
X          hostPath:
X            path: /mnt/collector
X        - name: varlog
X          hostPath:
X            path: /var/log
X        - name: varlibdockercontainers
X          hostPath:
X            path: /var/lib/docker/containers
SHAR_EOF
  (set 20 24 11 12 20 24 04 'collector.yaml'
   eval "${shar_touch}") && \
  chmod 0644 'collector.yaml'
if test $? -ne 0
then ${echo} "restore of collector.yaml failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'collector.yaml': 'MD5 check failed'
       ) << \SHAR_EOF
c80e5ee8d23e4af68f104f3194c0c90e  collector.yaml
SHAR_EOF

else
test `LC_ALL=C wc -c < 'collector.yaml'` -ne 11999 && \
  ${echo} "restoration warning:  size of 'collector.yaml' is not 11999"
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
  (set 20 24 08 28 01 41 04 'ksm.yaml'
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
X  - apiGroups:
X      - adx-mon.azure.com
X    resources:
X      - functions
X    verbs:
X      - get
X      - list
X  - apiGroups:
X      - adx-mon.azure.com
X    resources:
X      - functions/status
X    verbs:
X      - update
X      - patch
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
X        adx-mon/log-parsers: json
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
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
X  annotations:
X    controller-gen.kubebuilder.io/version: v0.16.1
X  name: functions.adx-mon.azure.com
spec:
X  group: adx-mon.azure.com
X  names:
X    kind: Function
X    listKind: FunctionList
X    plural: functions
X    singular: function
X  scope: Namespaced
X  versions:
X    - name: v1
X      schema:
X        openAPIV3Schema:
X          description: Function defines a KQL function to be maintained in the Kusto
X            cluster
X          properties:
X            apiVersion:
X              description: |-
X                APIVersion defines the versioned schema of this representation of an object.
X                Servers should convert recognized schemas to the latest internal value, and
X                may reject unrecognized values.
X                More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
X              type: string
X            kind:
X              description: |-
X                Kind is a string value representing the REST resource this object represents.
X                Servers may infer this from the endpoint the client submits requests to.
X                Cannot be updated.
X                In CamelCase.
X                More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
X              type: string
X            metadata:
X              type: object
X            spec:
X              description: FunctionSpec defines the desired state of Function
X              properties:
X                body:
X                  description: Body is the KQL body of the function
X                  type: string
X                database:
X                  description: Database is the name of the database in which the function
X                    will be created
X                  type: string
X              required:
X                - body
X                - database
X              type: object
X            status:
X              description: FunctionStatus defines the observed state of Function
X              properties:
X                error:
X                  description: Error is a string that communicates any error message
X                    if one exists
X                  type: string
X                lastTimeReconciled:
X                  description: LastTimeReconciled is the last time the Function was
X                    reconciled
X                  format: date-time
X                  type: string
X                message:
X                  description: Message is a human-readable message indicating details
X                    about the Function
X                  type: string
X                status:
X                  description: Status is an enum that represents the status of the Function
X                  type: string
X              required:
X                - lastTimeReconciled
X                - status
X              type: object
X          type: object
X      served: true
X      storage: true
X      subresources:
X        status: {}
SHAR_EOF
  (set 20 24 11 13 22 35 52 'ingestor.yaml'
   eval "${shar_touch}") && \
  chmod 0644 'ingestor.yaml'
if test $? -ne 0
then ${echo} "restore of ingestor.yaml failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'ingestor.yaml': 'MD5 check failed'
       ) << \SHAR_EOF
e902a9b8b7b725e6ec40f70263f4c368  ingestor.yaml
SHAR_EOF

else
test `LC_ALL=C wc -c < 'ingestor.yaml'` -ne 7692 && \
  ${echo} "restoration warning:  size of 'ingestor.yaml' is not 7692"
  fi
if rm -fr ${lock_dir}
then ${echo} "x - removed lock directory ${lock_dir}."
else ${echo} "x - failed to remove lock directory ${lock_dir}."
     exit 1
fi
./setup.sh
