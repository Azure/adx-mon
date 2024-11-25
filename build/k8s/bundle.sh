#!/bin/sh
# This is a shell archive (produced by GNU sharutils 4.15.2).
# To extract the files from this archive, save it to some FILE, remove
# everything before the '#!/bin/sh' line above, then type 'sh FILE'.
#
lock_dir=_sh00161
# Made on 2024-12-06 21:07 UTC by <root@c76e4d3b0522>.
# Source directory was '/build'.
#
# Existing files WILL be overwritten.
#
# This shar contains:
# length mode       name
# ------ ---------- ------------------------------------------
#   5804 -rw-r--r-- ksm.yaml
#  82148 -rw-r--r-- dashboards/metrics-stats.json
#  53133 -rw-r--r-- dashboards/cluster-info.json
#  65204 -rw-r--r-- dashboards/pods.json
#  37778 -rw-r--r-- dashboards/api-server.json
#  70780 -rw-r--r-- dashboards/namespaces.json
#   7692 -rw-r--r-- ingestor.yaml
#  10488 -rwxr-xr-x setup.sh
#  13101 -rw-r--r-- collector.yaml
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
  (set 20 24 10 05 02 33 17 'ksm.yaml'
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
# ============= dashboards/metrics-stats.json ==============
if test ! -d 'dashboards'; then
  mkdir 'dashboards' || exit 1
fi
  sed 's/^X//' << 'SHAR_EOF' | uudecode &&
begin 600 dashboards/metrics-stats.json
M>PT*("`B86YN;W1A=&EO;G,B.B![#0H@("`@(FQI<W0B.B!;#0H@("`@("![
M#0H@("`@("`@(")B=6EL=$EN(CH@,2P-"B`@("`@("`@(F1A=&%S;W5R8V4B
M.B![#0H@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X
M<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@(")U:60B.B`B)'M$871A
M<V]U<F-E?2(-"B`@("`@("`@?2P-"B`@("`@("`@(F5N86)L92(Z('1R=64L
M#0H@("`@("`@(")H:61E(CH@=')U92P-"B`@("`@("`@(FEC;VY#;VQO<B(Z
M(")R9V)A*#`L(#(Q,2P@,C4U+"`Q*2(L#0H@("`@("`@(")N86UE(CH@(D%N
M;F]T871I;VYS("8@06QE<G1S(BP-"B`@("`@("`@(G1A<F=E="(Z('L-"B`@
M("`@("`@("`B;&EM:70B.B`Q,#`L#0H@("`@("`@("`@(FUA=&-H06YY(CH@
M9F%L<V4L#0H@("`@("`@("`@(G1A9W,B.B!;72P-"B`@("`@("`@("`B='EP
M92(Z(")D87-H8F]A<F0B#0H@("`@("`@('TL#0H@("`@("`@(")T>7!E(CH@
M(F1A<VAB;V%R9"(-"B`@("`@('T-"B`@("!=#0H@('TL#0H@(")E9&ET86)L
M92(Z('1R=64L#0H@(")F:7-C86Q996%R4W1A<G1-;VYT:"(Z(#`L#0H@(")G
M<F%P:%1O;VQT:7`B.B`P+`T*("`B:60B.B`U,RP-"B`@(FQI;FMS(CH@6UTL
M#0H@(")L:79E3F]W(CH@9F%L<V4L#0H@(")P86YE;',B.B!;#0H@("`@>PT*
M("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A
M;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@
M(G5I9"(Z("(D>T1A=&%S;W5R8V5](@T*("`@("`@?2P-"B`@("`@(")F:65L
M9$-O;F9I9R(Z('L-"B`@("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@
M(")C;VQO<B(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@(G1H<F5S:&]L9',B
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;72P-"B`@
M("`@("`@("`B;6%X(CH@,3`P,#`L#0H@("`@("`@("`@(FUI;B(Z(#`L#0H@
M("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@("`@("`@("`@("`B;6]D92(Z
M(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B<W1E<',B.B!;#0H@("`@("`@
M("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B9W)E96XB+`T*
M("`@("`@("`@("`@("`@(")V86QU92(Z(&YU;&P-"B`@("`@("`@("`@("`@
M?2P-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z
M(")R960B+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(#DP,#`-"B`@("`@
M("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('T-"B`@("`@
M("`@?2P-"B`@("`@("`@(F]V97)R:61E<R(Z(%M=#0H@("`@("!]+`T*("`@
M("`@(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@-"P-"B`@("`@("`@(G<B
M.B`T+`T*("`@("`@("`B>"(Z(#`L#0H@("`@("`@(")Y(CH@,`T*("`@("`@
M?2P-"B`@("`@(")I9"(Z(#(L#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@
M("`@(FUI;E9I>DAE:6=H="(Z(#<U+`T*("`@("`@("`B;6EN5FEZ5VED=&@B
M.B`W-2P-"B`@("`@("`@(F]R:65N=&%T:6]N(CH@(F%U=&\B+`T*("`@("`@
M("`B<F5D=6-E3W!T:6]N<R(Z('L-"B`@("`@("`@("`B8V%L8W,B.B!;#0H@
M("`@("`@("`@("`B;&%S=$YO=$YU;&PB#0H@("`@("`@("`@72P-"B`@("`@
M("`@("`B9FEE;&1S(CH@(B(L#0H@("`@("`@("`@(G9A;'5E<R(Z(&9A;'-E
M#0H@("`@("`@('TL#0H@("`@("`@(")S:&]W5&AR97-H;VQD3&%B96QS(CH@
M9F%L<V4L#0H@("`@("`@(")S:&]W5&AR97-H;VQD36%R:V5R<R(Z('1R=64L
M#0H@("`@("`@(")S:7II;F<B.B`B875T;R(-"B`@("`@('TL#0H@("`@("`B
M<&QU9VEN5F5R<VEO;B(Z("(Q,"XT+C$Q(BP-"B`@("`@(")T87)G971S(CH@
M6PT*("`@("`@("![#0H@("`@("`@("`@(F1A=&%B87-E(CH@(B1$871A8F%S
M92(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B
M='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C
M92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@("`@
M("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@
M("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B
M.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@
M("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@
M(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A
M;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*
M("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@
M("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL
M#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-"XT+C$B+`T*("`@("`@
M("`@(")Q=65R>2(Z("(N<VAO=R!T86)L97,@9&5T86EL<UQN?"!S=6UM87)I
M>F4@9&-O=6YT*%1A8FQE3F%M92D@("(L#0H@("`@("`@("`@(G%U97)Y4V]U
M<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*
M("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B
M.B`B02(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T86)L92(-"B`@
M("`@("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z(")-971R:6-S(BP-
M"B`@("`@(")T>7!E(CH@(F=A=6=E(@T*("`@('TL#0H@("`@>PT*("`@("`@
M(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V5](@T*("`@("`@?2P-"B`@("`@(")F:65L9$-O;F9I
M9R(Z('L-"B`@("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@(")C;VQO
M<B(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@(G1H<F5S:&]L9',B#0H@("`@
M("`@("`@?2P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;72P-"B`@("`@("`@
M("`B=&AR97-H;VQD<R(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@(F%B<V]L
M=71E(BP-"B`@("`@("`@("`@(")S=&5P<R(Z(%L-"B`@("`@("`@("`@("`@
M>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@
M("`@("`@(")V86QU92(Z(&YU;&P-"B`@("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")G<F5E;B(L
M#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@,`T*("`@("`@("`@("`@("!]
M#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B=6YI
M="(Z(")S:&]R="(-"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R:61E<R(Z
M(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@("`@(")H
M(CH@-"P-"B`@("`@("`@(G<B.B`T+`T*("`@("`@("`B>"(Z(#0L#0H@("`@
M("`@(")Y(CH@,`T*("`@("`@?2P-"B`@("`@(")H:61E5&EM94]V97)R:61E
M(CH@=')U92P-"B`@("`@(")I9"(Z(#$T+`T*("`@("`@(FEN=&5R=F%L(CH@
M(C%M(BP-"B`@("`@(")O<'1I;VYS(CH@>PT*("`@("`@("`B8V]L;W)-;V1E
M(CH@(G9A;'5E(BP-"B`@("`@("`@(F=R87!H36]D92(Z(")N;VYE(BP-"B`@
M("`@("`@(FIU<W1I9GE-;V1E(CH@(F%U=&\B+`T*("`@("`@("`B;W)I96YT
M871I;VXB.B`B875T;R(L#0H@("`@("`@(")R961U8V5/<'1I;VYS(CH@>PT*
M("`@("`@("`@(")C86QC<R(Z(%L-"B`@("`@("`@("`@(")L87-T3F]T3G5L
M;"(-"B`@("`@("`@("!=+`T*("`@("`@("`@(")F:65L9',B.B`B+UY686QU
M920O(BP-"B`@("`@("`@("`B=F%L=65S(CH@9F%L<V4-"B`@("`@("`@?2P-
M"B`@("`@("`@(G-H;W=097)C96YT0VAA;F=E(CH@9F%L<V4L#0H@("`@("`@
M(")T97AT36]D92(Z(")A=71O(BP-"B`@("`@("`@(G=I9&5,87EO=70B.B!T
M<G5E#0H@("`@("!]+`T*("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B,3`N-"XQ
M,2(L#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@("`@
M(")D871A8F%S92(Z("(D1&%T86)A<V4B+`T*("`@("`@("`@(")D871A<V]U
M<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD
M871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V5](@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X
M<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@
M("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T
M>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D
M=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@
M("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I
M;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@
M("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G:6Y697)S
M:6]N(CH@(C0N-"XQ(BP-"B`@("`@("`@("`B<75E<GDB.B`B061X;6]N26YG
M97-T;W)486)L94-A<F1I;F%L:71Y0V]U;G1<;GP@=VAE<F4@5&EM97-T86UP
M(#X](&%G;R@S,&TI7&Y\(&5X=&5N9"!486)L93UT;W-T<FEN9RA,86)E;',N
M=&%B;&4I7&Y\('-U;6UA<FEZ92!686QU93UR;W5N9"AA=F<H5F%L=64I*2!B
M>2!B:6XH5&EM97-T86UP+"`Q;2DL(%1A8FQE7&Y\('-U;6UA<FEZ92!686QU
M93US=6TH5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L(#%M*5QN?"!O<F1E<B!B
M>2!4:6UE<W1A;7`@87-C("!<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E
M(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@
M("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B
M02(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T86)L92(-"B`@("`@
M("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z(")!8W1I=F4@4V5R:65S
M(BP-"B`@("`@(")T>7!E(CH@(G-T870B#0H@("`@?2P-"B`@("![#0H@("`@
M("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA
M>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED
M(CH@(B1[1&%T87-O=7)C97TB#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N
M9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@("`@("`@("`@(F-O
M;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B=&AR97-H;VQD<R(-"B`@
M("`@("`@("!]+`T*("`@("`@("`@(")D96-I;6%L<R(Z(#$L#0H@("`@("`@
M("`@(FUA<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![
M#0H@("`@("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@
M("`B<W1E<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@
M("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU92(Z
M(&YU;&P-"B`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@
M("`@('TL#0H@("`@("`@("`@(G5N:70B.B`B<VAO<G0B#0H@("`@("`@('TL
M#0H@("`@("`@(")O=F5R<FED97,B.B!;#0H@("`@("`@("`@>PT*("`@("`@
M("`@("`@(FUA=&-H97(B.B![#0H@("`@("`@("`@("`@(")I9"(Z(")B>4YA
M;64B+`T*("`@("`@("`@("`@("`B;W!T:6]N<R(Z(")!34-O<W0B#0H@("`@
M("`@("`@("!]+`T*("`@("`@("`@("`@(G!R;W!E<G1I97,B.B!;#0H@("`@
M("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B:60B.B`B=6YI="(L#0H@
M("`@("`@("`@("`@("`@(G9A;'5E(CH@(F-U<G)E;F-Y55-$(@T*("`@("`@
M("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?0T*("`@("`@
M("!=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@("`@(")H
M(CH@-"P-"B`@("`@("`@(G<B.B`T+`T*("`@("`@("`B>"(Z(#@L#0H@("`@
M("`@(")Y(CH@,`T*("`@("`@?2P-"B`@("`@(")I9"(Z(#,L#0H@("`@("`B
M;W!T:6]N<R(Z('L-"B`@("`@("`@(F-O;&]R36]D92(Z(")V86QU92(L#0H@
M("`@("`@(")G<F%P:$UO9&4B.B`B87)E82(L#0H@("`@("`@(")J=7-T:69Y
M36]D92(Z(")A=71O(BP-"B`@("`@("`@(F]R:65N=&%T:6]N(CH@(F%U=&\B
M+`T*("`@("`@("`B<F5D=6-E3W!T:6]N<R(Z('L-"B`@("`@("`@("`B8V%L
M8W,B.B!;#0H@("`@("`@("`@("`B;&%S=$YO=$YU;&PB#0H@("`@("`@("`@
M72P-"B`@("`@("`@("`B9FEE;&1S(CH@(B\N*B\B+`T*("`@("`@("`@(")V
M86QU97,B.B!F86QS90T*("`@("`@("!]+`T*("`@("`@("`B<VAO=U!E<F-E
M;G1#:&%N9V4B.B!F86QS92P-"B`@("`@("`@(G1E>'1-;V1E(CH@(F%U=&\B
M+`T*("`@("`@("`B=VED94QA>6]U="(Z('1R=64-"B`@("`@('TL#0H@("`@
M("`B<&QU9VEN5F5R<VEO;B(Z("(Q,"XT+C$Q(BP-"B`@("`@(")T87)G971S
M(CH@6PT*("`@("`@("![#0H@("`@("`@("`@(F1A=&%B87-E(CH@(B1$871A
M8F%S92(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@
M("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O
M=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@
M("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@
M("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO
M;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@
M("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@
M("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z
M(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@
M>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@
M("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@
M('TL#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-"XT+C$B+`T*("`@
M("`@("`@(")Q=65R>2(Z("(N<VAO=R!E>'1E;G1S(%QN?"!S=6UM87)I>F4@
M4V%M<&QE<SUS=6TH4F]W0V]U;G0I7&Y\(&5X=&5N9"!!34-O<W0]*%-A;7!L
M97,O,3!E-BDJ,"XQ-B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A
M=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@
M(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@
M("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T86)L92(-"B`@("`@("`@?0T*
M("`@("`@72P-"B`@("`@(")T:71L92(Z(")386UP;&5S(BP-"B`@("`@(")T
M>7!E(CH@(G-T870B#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O=7)C
M92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X
M<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T87-O
M=7)C97TB#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@
M("`@("`B9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@
M("`@("`@("`@(FUO9&4B.B`B=&AR97-H;VQD<R(-"B`@("`@("`@("!]+`T*
M("`@("`@("`@(")D96-I;6%L<R(Z(#$L#0H@("`@("`@("`@(FUA<'!I;F=S
M(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@("`@("`@("`@
M("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B<W1E<',B.B!;
M#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B
M9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(&YU;&P-"B`@("`@
M("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@("`@
M("`@("`@(G5N:70B.B`B<VAO<G0B#0H@("`@("`@('TL#0H@("`@("`@(")O
M=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@>PT*
M("`@("`@("`B:"(Z(#0L#0H@("`@("`@(")W(CH@-"P-"B`@("`@("`@(G@B
M.B`Q,BP-"B`@("`@("`@(GDB.B`P#0H@("`@("!]+`T*("`@("`@(FED(CH@
M,C@L#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(F-O;&]R36]D92(Z
M(")V86QU92(L#0H@("`@("`@(")G<F%P:$UO9&4B.B`B87)E82(L#0H@("`@
M("`@(")J=7-T:69Y36]D92(Z(")A=71O(BP-"B`@("`@("`@(F]R:65N=&%T
M:6]N(CH@(F%U=&\B+`T*("`@("`@("`B<F5D=6-E3W!T:6]N<R(Z('L-"B`@
M("`@("`@("`B8V%L8W,B.B!;#0H@("`@("`@("`@("`B;&%S=$YO=$YU;&PB
M#0H@("`@("`@("`@72P-"B`@("`@("`@("`B9FEE;&1S(CH@(B(L#0H@("`@
M("`@("`@(G9A;'5E<R(Z(&9A;'-E#0H@("`@("`@('TL#0H@("`@("`@(")S
M:&]W4&5R8V5N=$-H86YG92(Z(&9A;'-E+`T*("`@("`@("`B=&5X=$UO9&4B
M.B`B875T;R(L#0H@("`@("`@(")W:61E3&%Y;W5T(CH@=')U90T*("`@("`@
M?2P-"B`@("`@(")P;'5G:6Y697)S:6]N(CH@(C$P+C0N,3$B+`T*("`@("`@
M(G1A<F=E=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B9&%T86)A<V4B
M.B`B)$1A=&%B87-E(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@
M("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E
M<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U
M<F-E?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@
M>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E
M>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*
M("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@
M("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(T+C0N
M,2(L#0H@("`@("`@("`@(G%U97)Y(CH@(D%D>&UO;D-O;&QE8W1O<DAE86QT
M:$-H96-K7&Y\('=H97)E(%1I;65S=&%M<"`^/2!A9V\H,S!M*5QN?"!S=6UM
M87)I>F4@9&-O=6YT*$AO<W0I(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B
M.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@
M("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")!
M(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1A8FQE(@T*("`@("`@
M("!]#0H@("`@("!=+`T*("`@("`@(G1I=&QE(CH@(DAO<W1S(BP-"B`@("`@
M(")T>7!E(CH@(G-T870B#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O
M=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A
M+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T
M87-O=7)C97TB#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*
M("`@("`@("`B9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*
M("`@("`@("`@("`@(FUO9&4B.B`B=&AR97-H;VQD<R(-"B`@("`@("`@("!]
M+`T*("`@("`@("`@(")D96-I;6%L<R(Z(#$L#0H@("`@("`@("`@(FUA<'!I
M;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@("`@("`@
M("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B<W1E<',B
M.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B
M.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(&YU;&P-"B`@
M("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(G5N:70B.B`B<VAO<G0B#0H@("`@("`@('TL#0H@("`@("`@
M(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@
M>PT*("`@("`@("`B:"(Z(#0L#0H@("`@("`@(")W(CH@-"P-"B`@("`@("`@
M(G@B.B`Q-BP-"B`@("`@("`@(GDB.B`P#0H@("`@("!]+`T*("`@("`@(FED
M(CH@,CDL#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(F-O;&]R36]D
M92(Z(")V86QU92(L#0H@("`@("`@(")G<F%P:$UO9&4B.B`B;F]N92(L#0H@
M("`@("`@(")J=7-T:69Y36]D92(Z(")A=71O(BP-"B`@("`@("`@(F]R:65N
M=&%T:6]N(CH@(F%U=&\B+`T*("`@("`@("`B<F5D=6-E3W!T:6]N<R(Z('L-
M"B`@("`@("`@("`B8V%L8W,B.B!;#0H@("`@("`@("`@("`B;6%X(@T*("`@
M("`@("`@(%TL#0H@("`@("`@("`@(F9I96QD<R(Z("(O7E9A;'5E)"\B+`T*
M("`@("`@("`@(")V86QU97,B.B!F86QS90T*("`@("`@("!]+`T*("`@("`@
M("`B<VAO=U!E<F-E;G1#:&%N9V4B.B!F86QS92P-"B`@("`@("`@(G1E>'1-
M;V1E(CH@(F%U=&\B+`T*("`@("`@("`B=VED94QA>6]U="(Z('1R=64-"B`@
M("`@('TL#0H@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(Q,"XT+C$Q(BP-"B`@
M("`@(")T87)G971S(CH@6PT*("`@("`@("![#0H@("`@("`@("`@(F1A=&%B
M87-E(CH@(B1$871A8F%S92(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![
M#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP
M;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T
M87-O=7)C97TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO
M;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@
M("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B
M86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![
M#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@
M("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@
M("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;
M72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@
M?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B
M-"XT+C$B+`T*("`@("`@("`@(")Q=65R>2(Z(")#;VYT86EN97)#<'55<V%G
M95-E8V]N9'-4;W1A;%QN?"!W:&5R92!4:6UE<W1A;7`@/CT@86=O*#,P;2E<
M;GP@=VAE<F4@0VQU<W1E<B`]/2!<(B1#;'5S=&5R7")<;GP@9&ES=&EN8W0@
M=&]S=')I;F<H3&%B96QS+G!O9"E<;GP@<W5M;6%R:7IE(%9A;'5E/6-O=6YT
M*"E<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@
M("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E
M(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@("`@("`@("`@
M(G)E<W5L=$9O<FUA="(Z(")T86)L92(-"B`@("`@("`@?0T*("`@("`@72P-
M"B`@("`@(")T:71L92(Z(")0;V1S(BP-"B`@("`@(")T>7!E(CH@(G-T870B
M#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@
M("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S
M;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@("`@
M("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L
M=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO
M9&4B.B`B=&AR97-H;VQD<R(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")M
M87!P:6YG<R(Z(%M=+`T*("`@("`@("`@(")T:')E<VAO;&1S(CH@>PT*("`@
M("`@("`@("`@(FUO9&4B.B`B86)S;VQU=&4B+`T*("`@("`@("`@("`@(G-T
M97!S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O
M;&]R(CH@(G)E9"(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@;G5L;`T*
M("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@("![#0H@("`@("`@("`@
M("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@("`@("`@("`B=F%L
M=64B.B`P#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@
M("`@("!]+`T*("`@("`@("`@(")U;FET(CH@(G=P<R(-"B`@("`@("`@?2P-
M"B`@("`@("`@(F]V97)R:61E<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R
M:610;W,B.B![#0H@("`@("`@(")H(CH@-"P-"B`@("`@("`@(G<B.B`T+`T*
M("`@("`@("`B>"(Z(#(P+`T*("`@("`@("`B>2(Z(#`-"B`@("`@('TL#0H@
M("`@("`B:&ED951I;65/=F5R<FED92(Z('1R=64L#0H@("`@("`B:60B.B`Q
M,RP-"B`@("`@(")I;G1E<G9A;"(Z("(Q;2(L#0H@("`@("`B;W!T:6]N<R(Z
M('L-"B`@("`@("`@(F-O;&]R36]D92(Z(")V86QU92(L#0H@("`@("`@(")G
M<F%P:$UO9&4B.B`B;F]N92(L#0H@("`@("`@(")J=7-T:69Y36]D92(Z(")A
M=71O(BP-"B`@("`@("`@(F]R:65N=&%T:6]N(CH@(F%U=&\B+`T*("`@("`@
M("`B<F5D=6-E3W!T:6]N<R(Z('L-"B`@("`@("`@("`B8V%L8W,B.B!;#0H@
M("`@("`@("`@("`B;65A;B(-"B`@("`@("`@("!=+`T*("`@("`@("`@(")F
M:65L9',B.B`B+UY686QU920O(BP-"B`@("`@("`@("`B=F%L=65S(CH@9F%L
M<V4-"B`@("`@("`@?2P-"B`@("`@("`@(G-H;W=097)C96YT0VAA;F=E(CH@
M9F%L<V4L#0H@("`@("`@(")T97AT36]D92(Z(")A=71O(BP-"B`@("`@("`@
M(G=I9&5,87EO=70B.B!T<G5E#0H@("`@("!]+`T*("`@("`@(G!L=6=I;E9E
M<G-I;VXB.B`B,3`N-"XQ,2(L#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@
M("`@>PT*("`@("`@("`@(")D871A8F%S92(Z("(D1&%T86)A<V4B+`T*("`@
M("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B
M9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@
M("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V5](@T*("`@("`@("`@('TL
M#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O
M=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@
M("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S
M<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@
M("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@
M("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E
M(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@
M("`@(")P;'5G:6Y697)S:6]N(CH@(C0N-"XQ(BP-"B`@("`@("`@("`B<75E
M<GDB.B`B061X;6]N26YG97-T;W)386UP;&5S4W1O<F5D5&]T86Q<;GP@=VAE
M<F4@5&EM97-T86UP(#X](&%G;R@S,&TI7&Y\('=H97)E(%1I;65S=&%M<"`\
M(&%G;R@U;2E<;GP@=VAE<F4@0V]N=&%I;F5R(#T](%PB:6YG97-T;W)<(EQN
M?"!I;G9O:V4@<')O;5]D96QT82@I7&Y\('-U;6UA<FEZ92!686QU93US=6TH
M5F%L=64I+S8P(&)Y(&)I;BA4:6UE<W1A;7`L(#%M*2P@4&]D7&Y\('-U;6UA
M<FEZ92!686QU93US=6TH5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L(#%M*5QN
M7&Y<;EQN7&XB+`T*("`@("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*
M("`@("`@("`@(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W
M36]D92(Z('1R=64L#0H@("`@("`@("`@(G)E9DED(CH@(D$B+`T*("`@("`@
M("`@(")R97-U;'1&;W)M870B.B`B=&%B;&4B#0H@("`@("`@('T-"B`@("`@
M(%TL#0H@("`@("`B=&EM95-H:69T(CH@(C5M(BP-"B`@("`@(")T:71L92(Z
M(")386UP;&5S(%1H<F]U9VAP=70B+`T*("`@("`@(G1Y<&4B.B`B<W1A="(-
M"B`@("!]+`T*("`@('L-"B`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@
M("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O
M=7)C92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E?2(-"B`@("`@
M('TL#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@("`@(")D969A=6QT
M<R(Z('L-"B`@("`@("`@("`B8V]L;W(B.B![#0H@("`@("`@("`@("`B;6]D
M92(Z(")T:')E<VAO;&1S(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(FUA
M<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@("`@
M("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B<W1E
M<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L
M;W(B.B`B8FQU92(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@;G5L;`T*
M("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@("![#0H@("`@("`@("`@
M("`@("`@(F-O;&]R(CH@(G)E9"(L#0H@("`@("`@("`@("`@("`@(G9A;'5E
M(CH@-C`-"B`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@
M("`@('TL#0H@("`@("`@("`@(G5N:70B.B`B<R(-"B`@("`@("`@?2P-"B`@
M("`@("`@(F]V97)R:61E<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610
M;W,B.B![#0H@("`@("`@(")H(CH@-"P-"B`@("`@("`@(G<B.B`T+`T*("`@
M("`@("`B>"(Z(#`L#0H@("`@("`@(")Y(CH@-`T*("`@("`@?2P-"B`@("`@
M(")H:61E5&EM94]V97)R:61E(CH@=')U92P-"B`@("`@(")I9"(Z(#$Y+`T*
M("`@("`@(FEN=&5R=F%L(CH@(C%M(BP-"B`@("`@(")O<'1I;VYS(CH@>PT*
M("`@("`@("`B8V]L;W)-;V1E(CH@(G9A;'5E(BP-"B`@("`@("`@(F=R87!H
M36]D92(Z(")N;VYE(BP-"B`@("`@("`@(FIU<W1I9GE-;V1E(CH@(F-E;G1E
M<B(L#0H@("`@("`@(")O<FEE;G1A=&EO;B(Z(")A=71O(BP-"B`@("`@("`@
M(G)E9'5C94]P=&EO;G,B.B![#0H@("`@("`@("`@(F-A;&-S(CH@6PT*("`@
M("`@("`@("`@(F9I<G-T3F]T3G5L;"(-"B`@("`@("`@("!=+`T*("`@("`@
M("`@(")F:65L9',B.B`B+UY686QU920O(BP-"B`@("`@("`@("`B=F%L=65S
M(CH@9F%L<V4-"B`@("`@("`@?2P-"B`@("`@("`@(G-H;W=097)C96YT0VAA
M;F=E(CH@9F%L<V4L#0H@("`@("`@(")T97AT36]D92(Z(")A=71O(BP-"B`@
M("`@("`@(G=I9&5,87EO=70B.B!T<G5E#0H@("`@("!]+`T*("`@("`@(G!L
M=6=I;E9E<G-I;VXB.B`B,3`N-"XQ,2(L#0H@("`@("`B=&%R9V5T<R(Z(%L-
M"B`@("`@("`@>PT*("`@("`@("`@(")D871A8F%S92(Z("(D1&%T86)A<V4B
M+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y
M<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B
M+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V5](@T*("`@("`@
M("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@
M("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@
M6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@
M('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B
M97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD
M(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*
M("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C0N-"XQ(BP-"B`@("`@("`@
M("`B<75E<GDB.B`B061X;6]N26YG97-T;W)786Q396=M96YT<TUA>$%G95-E
M8V]N9'-<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H
M97)E($-L=7-T97(@/3T@7"(D0VQU<W1E<EPB7&Y\('=H97)E(%!O9"!S=&%R
M='-W:71H(%PB:6YG97-T;W)<(EQN?"!W:&5R92!686QU92`A/2`Y,C(S,S<R
M,#,V+C@U-#<W-C1<;GP@;6%K92US97)I97,@5F%L=64]879G*%9A;'5E*2!O
M;B!4:6UE<W1A;7`@9G)O;2!A9V\H,S!M*2!T;R!N;W<H*2!S=&5P(#8P,#`P
M;7-<;GP@97AT96YD(&UV9SUS97)I97-?9FER*%9A;'5E+"!R97!E870H,2P@
M-2DL('1R=64L('1R=64I7&Y\(&5X=&5N9"!M=F<]87)R87E?<VQI8V4H;79G
M+"`P+"!A<G)A>5]L96YG=&@H;79G*2TU*5QN?"!E>'1E;F0@5F%L=64];79G
M6V%R<F%Y7VQE;F=T:"AM=F<I+3%=7&Y\('!R;VIE8W0@5F%L=65<;B(L#0H@
M("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U
M97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-
M"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@("`@("`@("`@(G)E<W5L=$9O
M<FUA="(Z(")T86)L92(-"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T
M:6UE4VAI9G0B.B`B,6TB+`T*("`@("`@(G1I=&QE(CH@(D%V9R!396=M96YT
M($%G92(L#0H@("`@("`B='EP92(Z(")S=&%T(@T*("`@('TL#0H@("`@>PT*
M("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A
M;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@
M(G5I9"(Z("(D>T1A=&%S;W5R8V5](@T*("`@("`@?2P-"B`@("`@(")F:65L
M9$-O;F9I9R(Z('L-"B`@("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@
M(")C;VQO<B(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@(G1H<F5S:&]L9',B
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;72P-"B`@
M("`@("`@("`B=&AR97-H;VQD<R(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@
M(F%B<V]L=71E(BP-"B`@("`@("`@("`@(")S=&5P<R(Z(%L-"B`@("`@("`@
M("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")B;'5E(BP-"B`@
M("`@("`@("`@("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B
M<F5D(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`V,`T*("`@("`@("`@
M("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@("`@
M("`B=6YI="(Z(")S(@T*("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S
M(CH@6UT-"B`@("`@('TL#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@
M(F@B.B`T+`T*("`@("`@("`B=R(Z(#0L#0H@("`@("`@(")X(CH@-"P-"B`@
M("`@("`@(GDB.B`T#0H@("`@("!]+`T*("`@("`@(FAI9&54:6UE3W9E<G)I
M9&4B.B!T<G5E+`T*("`@("`@(FED(CH@,C0L#0H@("`@("`B:6YT97)V86PB
M.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@("`@(")C;VQO<DUO
M9&4B.B`B=F%L=64B+`T*("`@("`@("`B9W)A<&A-;V1E(CH@(FYO;F4B+`T*
M("`@("`@("`B:G5S=&EF>4UO9&4B.B`B8V5N=&5R(BP-"B`@("`@("`@(F]R
M:65N=&%T:6]N(CH@(F%U=&\B+`T*("`@("`@("`B<F5D=6-E3W!T:6]N<R(Z
M('L-"B`@("`@("`@("`B8V%L8W,B.B!;#0H@("`@("`@("`@("`B9FER<W0B
M#0H@("`@("`@("`@72P-"B`@("`@("`@("`B9FEE;&1S(CH@(B]>5F%L=64D
M+R(L#0H@("`@("`@("`@(G9A;'5E<R(Z(&9A;'-E#0H@("`@("`@('TL#0H@
M("`@("`@(")S:&]W4&5R8V5N=$-H86YG92(Z(&9A;'-E+`T*("`@("`@("`B
M=&5X=$UO9&4B.B`B875T;R(L#0H@("`@("`@(")W:61E3&%Y;W5T(CH@=')U
M90T*("`@("`@?2P-"B`@("`@(")P;'5G:6Y697)S:6]N(CH@(C$P+C0N,3$B
M+`T*("`@("`@(G1A<F=E=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B
M9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B
M.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M
M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[
M1&%T87-O=7)C97TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S
M<VEO;B(Z('L-"B`@("`@("`@("`@(")F<F]M(CH@>PT*("`@("`@("`@("`@
M("`B<')O<&5R='DB.B![#0H@("`@("`@("`@("`@("`@(FYA;64B.B`B061X
M;6]N26YG97-T;W)297%U97-T<U)E8V5I=F5D5&]T86PB+`T*("`@("`@("`@
M("`@("`@(")T>7!E(CH@(G-T<FEN9R(-"B`@("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@("`@(G1Y<&4B.B`B<')O<&5R='DB#0H@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E
M>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%L-"B`@
M("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@("`B97AP<F5S<VEO
M;G,B.B!;#0H@("`@("`@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@
M("`@("`@("`B;W!E<F%T;W(B.B![#0H@("`@("`@("`@("`@("`@("`@("`@
M("`B;F%M92(Z("(]/2(L#0H@("`@("`@("`@("`@("`@("`@("`@("`B=F%L
M=64B.B`B(@T*("`@("`@("`@("`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`@("`@("`@("`@(G!R;W!E<G1Y(CH@>PT*("`@("`@("`@("`@("`@("`@
M("`@("`@(FYA;64B.B`B56YD97)L87E.86UE(BP-"B`@("`@("`@("`@("`@
M("`@("`@("`@(")T>7!E(CH@(G-T<FEN9R(-"B`@("`@("`@("`@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@("`@("`@("`@(")T>7!E(CH@(F]P97)A
M=&]R(@T*("`@("`@("`@("`@("`@("`@("!]#0H@("`@("`@("`@("`@("`@
M("!=+`T*("`@("`@("`@("`@("`@("`@(G1Y<&4B.B`B;W(B#0H@("`@("`@
M("`@("`@("`@?0T*("`@("`@("`@("`@("!=+`T*("`@("`@("`@("`@("`B
M='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@
M("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(T+C0N,2(L#0H@("`@("`@("`@
M(G%U97)Y(CH@(D%D>&UO;DEN9V5S=&]R5V%L4V5G;65N='--87A!9V5396-O
M;F1S7&Y\('=H97)E(%1I;65S=&%M<"`^/2!A9V\H,S!M*2!A;F0@5&EM97-T
M86UP(#P](&YO=R@I7&Y\('=H97)E($-L=7-T97(@/3T@7"(D0VQU<W1E<EPB
M7&Y\('=H97)E($-O;G1A:6YE<B`]/2!<(FEN9V5S=&]R7")<;GP@=VAE<F4@
M5F%L=64@(3T@.3(R,S,W,C`S-BXX-30W-S8T7&Y\(&UA:V4M<V5R:65S(%9A
M;'5E/6UA>"A686QU92D@;VX@5&EM97-T86UP(&9R;VT@86=O*#,P;2D@=&\@
M;F]W*"D@<W1E<"`V,#`P,&US7&Y\(&5X=&5N9"!M=F<]<V5R:65S7V9I<BA6
M86QU92P@<F5P96%T*#$L(#4I+"!T<G5E+"!T<G5E*5QN?"!E>'1E;F0@;79G
M/6%R<F%Y7W-L:6-E*&UV9RP@,"P@87)R87E?;&5N9W1H*&UV9RDM-2E<;GP@
M97AT96YD(%9A;'5E/6UV9UMA<G)A>5]L96YG=&@H;79G*2TQ75QN?"!P<F]J
M96-T(%9A;'5E(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-
M"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A
M=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")!(BP-"B`@("`@
M("`@("`B<F5S=6QT1F]R;6%T(CH@(G1A8FQE(@T*("`@("`@("!]#0H@("`@
M("!=+`T*("`@("`@(G1I;653:&EF="(Z("(Q;2(L#0H@("`@("`B=&ET;&4B
M.B`B36%X(%-E9VUE;G0@06=E(BP-"B`@("`@(")T>7!E(CH@(G-T870B#0H@
M("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@
M(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R
M8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@("`@("!]
M+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B
M.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B
M.B`B=&AR97-H;VQD<R(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")M87!P
M:6YG<R(Z(%M=+`T*("`@("`@("`@(")T:')E<VAO;&1S(CH@>PT*("`@("`@
M("`@("`@(FUO9&4B.B`B86)S;VQU=&4B+`T*("`@("`@("`@("`@(G-T97!S
M(CH@6PT*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R
M(CH@(F)L=64B+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(&YU;&P-"B`@
M("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@
M("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@("`@("`@(")V86QU92(Z
M(#$P,#`P,#`P,#`P#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-
M"B`@("`@("`@("!]+`T*("`@("`@("`@(")U;FET(CH@(F1E8V)Y=&5S(@T*
M("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6UT-"B`@("`@('TL
M#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`T+`T*("`@("`@
M("`B=R(Z(#0L#0H@("`@("`@(")X(CH@."P-"B`@("`@("`@(GDB.B`T#0H@
M("`@("!]+`T*("`@("`@(FED(CH@,C`L#0H@("`@("`B:6YT97)V86PB.B`B
M,6TB+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@("`@(")C;VQO<DUO9&4B
M.B`B=F%L=64B+`T*("`@("`@("`B9W)A<&A-;V1E(CH@(F%R96$B+`T*("`@
M("`@("`B:G5S=&EF>4UO9&4B.B`B875T;R(L#0H@("`@("`@(")O<FEE;G1A
M=&EO;B(Z(")A=71O(BP-"B`@("`@("`@(G)E9'5C94]P=&EO;G,B.B![#0H@
M("`@("`@("`@(F-A;&-S(CH@6PT*("`@("`@("`@("`@(FQA<W1.;W1.=6QL
M(@T*("`@("`@("`@(%TL#0H@("`@("`@("`@(F9I96QD<R(Z("(B+`T*("`@
M("`@("`@(")V86QU97,B.B!F86QS90T*("`@("`@("!]+`T*("`@("`@("`B
M<VAO=U!E<F-E;G1#:&%N9V4B.B!F86QS92P-"B`@("`@("`@(G1E>'1-;V1E
M(CH@(F%U=&\B+`T*("`@("`@("`B=VED94QA>6]U="(Z('1R=64-"B`@("`@
M('TL#0H@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(Q,"XT+C$Q(BP-"B`@("`@
M(")T87)G971S(CH@6PT*("`@("`@("![#0H@("`@("`@("`@(F1A=&%B87-E
M(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@
M("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R
M+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R
M8V5](@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![
M#0H@("`@("`@("`@("`B9G)O;2(Z('L-"B`@("`@("`@("`@("`@(G!R;W!E
M<G1Y(CH@>PT*("`@("`@("`@("`@("`@(")N86UE(CH@(D%D>&UO;DEN9V5S
M=&]R4F5Q=65S='-296-E:79E9%1O=&%L(BP-"B`@("`@("`@("`@("`@("`B
M='EP92(Z(")S=')I;F<B#0H@("`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`@(")T>7!E(CH@(G!R;W!E<G1Y(@T*("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO
M;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@
M("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@
M("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z
M(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@
M>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;#0H@("`@("`@("`@
M("`@("`@>PT*("`@("`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6PT*
M("`@("`@("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@("`@("`@
M(F]P97)A=&]R(CH@>PT*("`@("`@("`@("`@("`@("`@("`@("`@(FYA;64B
M.B`B/3TB+`T*("`@("`@("`@("`@("`@("`@("`@("`@(G9A;'5E(CH@(B(-
M"B`@("`@("`@("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@("`@("`@
M("`@(")P<F]P97)T>2(Z('L-"B`@("`@("`@("`@("`@("`@("`@("`@(")N
M86UE(CH@(E5N9&5R;&%Y3F%M92(L#0H@("`@("`@("`@("`@("`@("`@("`@
M("`B='EP92(Z(")S=')I;F<B#0H@("`@("`@("`@("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@("`@("`@("`@("`B='EP92(Z(")O<&5R871O<B(-"B`@
M("`@("`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@("`@("`@72P-"B`@
M("`@("`@("`@("`@("`@(")T>7!E(CH@(F]R(@T*("`@("`@("`@("`@("`@
M('T-"B`@("`@("`@("`@("`@72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B
M86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@
M(G!L=6=I;E9E<G-I;VXB.B`B-"XT+C$B+`T*("`@("`@("`@(")Q=65R>2(Z
M(")!9'AM;VY);F=E<W1O<E=A;%-E9VUE;G1S4VEZ94)Y=&5S7&Y\('=H97)E
M("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R(#T]
M(%PB)$-L=7-T97)<(EQN?"!E>'1E;F0@;65T<FEC/71O<W1R:6YG*$QA8F5L
M<RYM971R:6,I7&Y\('-U;6UA<FEZ92!686QU93UM87@H5F%L=64I(&)Y(&)I
M;BA4:6UE<W1A;7`L(#5M*2P@;65T<FEC+"!(;W-T7&Y\('-U;6UA<FEZ92!6
M86QU93US=6TH5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L(#5M*2P@2&]S=%QN
M?"!S=6UM87)I>F4@879G*%9A;'5E*2!B>2!4:6UE<W1A;7!<;GP@;W)D97(@
M8GD@5&EM97-T86UP(&%S8UQN7&XB+`T*("`@("`@("`@(")Q=65R>5-O=7)C
M92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@
M("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@("`@("`@("`@(G)E9DED(CH@
M(D$B+`T*("`@("`@("`@(")R97-U;'1&;W)M870B.B`B=&EM95]S97)I97,B
M#0H@("`@("`@('T-"B`@("`@(%TL#0H@("`@("`B=&ET;&4B.B`B5T%,(%-E
M9VUE;G1S($1I<VL@57-A9V4B+`T*("`@("`@(G1Y<&4B.B`B<W1A="(-"B`@
M("!]+`T*("`@('L-"B`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`B
M='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C
M92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E?2(-"B`@("`@('TL
M#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z
M('L-"B`@("`@("`@("`B8V]L;W(B.B![#0H@("`@("`@("`@("`B;6]D92(Z
M(")T:')E<VAO;&1S(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(FUA<'!I
M;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@("`@("`@
M("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B<W1E<',B
M.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B
M.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(&YU;&P-"B`@
M("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(G5N:70B.B`B8GET97,B#0H@("`@("`@('TL#0H@("`@("`@
M(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@
M>PT*("`@("`@("`B:"(Z(#0L#0H@("`@("`@(")W(CH@-"P-"B`@("`@("`@
M(G@B.B`Q,BP-"B`@("`@("`@(GDB.B`T#0H@("`@("!]+`T*("`@("`@(FED
M(CH@,C$L#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(F-O;&]R36]D
M92(Z(")V86QU92(L#0H@("`@("`@(")G<F%P:$UO9&4B.B`B87)E82(L#0H@
M("`@("`@(")J=7-T:69Y36]D92(Z(")A=71O(BP-"B`@("`@("`@(F]R:65N
M=&%T:6]N(CH@(F%U=&\B+`T*("`@("`@("`B<F5D=6-E3W!T:6]N<R(Z('L-
M"B`@("`@("`@("`B8V%L8W,B.B!;#0H@("`@("`@("`@("`B;&%S=$YO=$YU
M;&PB#0H@("`@("`@("`@72P-"B`@("`@("`@("`B9FEE;&1S(CH@(B]>4F5N
M=&EO;B0O(BP-"B`@("`@("`@("`B=F%L=65S(CH@9F%L<V4-"B`@("`@("`@
M?2P-"B`@("`@("`@(G-H;W=097)C96YT0VAA;F=E(CH@9F%L<V4L#0H@("`@
M("`@(")T97AT36]D92(Z(")A=71O(BP-"B`@("`@("`@(G=I9&5,87EO=70B
M.B!T<G5E#0H@("`@("!]+`T*("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B,3`N
M-"XQ,2(L#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@
M("`@(")D871A8F%S92(Z("(D1&%T86)A<V4B+`T*("`@("`@("`@(")D871A
M<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R
M92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I
M9"(Z("(D>T1A=&%S;W5R8V5](@T*("`@("`@("`@('TL#0H@("`@("`@("`@
M(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-
M"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E
M<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@
M("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G:6Y6
M97)S:6]N(CH@(C0N-"XQ(BP-"B`@("`@("`@("`B<75E<GDB.B`B+G-H;W<@
M97AT96YT<R!<;GP@<W5M;6%R:7IE($UI;D-R96%T93UM:6XH36EN0W)E871E
M9$]N*2P@36%X0W)E871E/6UA>"A-87A#<F5A=&5D3VXI7&Y\(&5X=&5N9"!4
M:6UE4F%N9V4]36%X0W)E871E("T@36EN0W)E871E7&Y\(&5X=&5N9"!296YT
M:6]N/7-T<F-A="AF;W)M871?=&EM97-P86XH5&EM95)A;F=E+"`G9&0G*2P@
M7"(@9&%Y<UPB*5QN?"!P<F]J96-T+6%W87D@36EN0W)E871E+"!-87A#<F5A
M=&4L(%1I;65286YG92(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A
M=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@
M(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@
M("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T86)L92(-"B`@("`@("`@?0T*
M("`@("`@72P-"B`@("`@(")T:71L92(Z(")296YT96YT:6]N(BP-"B`@("`@
M(")T>7!E(CH@(G-T870B#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O
M=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A
M+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T
M87-O=7)C97TB#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*
M("`@("`@("`B9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*
M("`@("`@("`@("`@(FUO9&4B.B`B=&AR97-H;VQD<R(-"B`@("`@("`@("!]
M+`T*("`@("`@("`@(")M87!P:6YG<R(Z(%M=+`T*("`@("`@("`@(")T:')E
M<VAO;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B86)S;VQU=&4B+`T*
M("`@("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@
M("`@("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@("`@("`@
M("`B=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@
M(%T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")U;FET(CH@(F)Y=&5S(@T*
M("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6UT-"B`@("`@('TL
M#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`T+`T*("`@("`@
M("`B=R(Z(#0L#0H@("`@("`@(")X(CH@,38L#0H@("`@("`@(")Y(CH@-`T*
M("`@("`@?2P-"B`@("`@(")I9"(Z(#<L#0H@("`@("`B;W!T:6]N<R(Z('L-
M"B`@("`@("`@(F-O;&]R36]D92(Z(")V86QU92(L#0H@("`@("`@(")G<F%P
M:$UO9&4B.B`B87)E82(L#0H@("`@("`@(")J=7-T:69Y36]D92(Z(")A=71O
M(BP-"B`@("`@("`@(F]R:65N=&%T:6]N(CH@(F%U=&\B+`T*("`@("`@("`B
M<F5D=6-E3W!T:6]N<R(Z('L-"B`@("`@("`@("`B8V%L8W,B.B!;#0H@("`@
M("`@("`@("`B;&%S=$YO=$YU;&PB#0H@("`@("`@("`@72P-"B`@("`@("`@
M("`B9FEE;&1S(CH@(B(L#0H@("`@("`@("`@(G9A;'5E<R(Z(&9A;'-E#0H@
M("`@("`@('TL#0H@("`@("`@(")S:&]W4&5R8V5N=$-H86YG92(Z(&9A;'-E
M+`T*("`@("`@("`B=&5X=$UO9&4B.B`B875T;R(L#0H@("`@("`@(")W:61E
M3&%Y;W5T(CH@=')U90T*("`@("`@?2P-"B`@("`@(")P;'5G:6Y697)S:6]N
M(CH@(C$P+C0N,3$B+`T*("`@("`@(G1A<F=E=',B.B!;#0H@("`@("`@('L-
M"B`@("`@("`@("`B9&%T86)A<V4B.B`B)$1A=&%B87-E(BP-"B`@("`@("`@
M("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A
M;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@
M("`@(")U:60B.B`B)'M$871A<V]U<F-E?2(-"B`@("`@("`@("!]+`T*("`@
M("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB
M.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@
M("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@
M("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS
M(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@
M("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@
M(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A
M;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B
M<&QU9VEN5F5R<VEO;B(Z("(T+C0N,2(L#0H@("`@("`@("`@(G%U97)Y(CH@
M(BYS:&]W(&5X=&5N=',@7&Y\('-U;6UA<FEZ92!S=6TH17AT96YT4VEZ92M)
M;F1E>%-I>F4I(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-
M"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A
M=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")!(BP-"B`@("`@
M("`@("`B<F5S=6QT1F]R;6%T(CH@(G1A8FQE(@T*("`@("`@("!]#0H@("`@
M("!=+`T*("`@("`@(G1I=&QE(CH@(D1"(%-I>F4B+`T*("`@("`@(G1Y<&4B
M.B`B<W1A="(-"B`@("!]+`T*("`@('L-"B`@("`@(")D871A<V]U<F-E(CH@
M>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R
M97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E
M?2(-"B`@("`@('TL#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@("`@
M(")D969A=6QT<R(Z('L-"B`@("`@("`@("`B8V]L;W(B.B![#0H@("`@("`@
M("`@("`B;6]D92(Z(")T:')E<VAO;&1S(@T*("`@("`@("`@('TL#0H@("`@
M("`@("`@(FUA<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B
M.B![#0H@("`@("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@
M("`@("`B<W1E<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@
M("`@("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU
M92(Z(&YU;&P-"B`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@
M("`@("`@('TL#0H@("`@("`@("`@(G5N:70B.B`B8GET97,B#0H@("`@("`@
M('TL#0H@("`@("`@(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@
M(")G<FED4&]S(CH@>PT*("`@("`@("`B:"(Z(#0L#0H@("`@("`@(")W(CH@
M-"P-"B`@("`@("`@(G@B.B`R,"P-"B`@("`@("`@(GDB.B`T#0H@("`@("!]
M+`T*("`@("`@(FED(CH@-BP-"B`@("`@(")O<'1I;VYS(CH@>PT*("`@("`@
M("`B8V]L;W)-;V1E(CH@(G9A;'5E(BP-"B`@("`@("`@(F=R87!H36]D92(Z
M(")A<F5A(BP-"B`@("`@("`@(FIU<W1I9GE-;V1E(CH@(F%U=&\B+`T*("`@
M("`@("`B;W)I96YT871I;VXB.B`B875T;R(L#0H@("`@("`@(")R961U8V5/
M<'1I;VYS(CH@>PT*("`@("`@("`@(")C86QC<R(Z(%L-"B`@("`@("`@("`@
M(")L87-T3F]T3G5L;"(-"B`@("`@("`@("!=+`T*("`@("`@("`@(")F:65L
M9',B.B`B(BP-"B`@("`@("`@("`B=F%L=65S(CH@9F%L<V4-"B`@("`@("`@
M?2P-"B`@("`@("`@(G-H;W=097)C96YT0VAA;F=E(CH@9F%L<V4L#0H@("`@
M("`@(")T97AT36]D92(Z(")A=71O(BP-"B`@("`@("`@(G=I9&5,87EO=70B
M.B!T<G5E#0H@("`@("!]+`T*("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B,3`N
M-"XQ,2(L#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@
M("`@(")D871A8F%S92(Z("(D1&%T86)A<V4B+`T*("`@("`@("`@(")D871A
M<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R
M92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I
M9"(Z("(D>T1A=&%S;W5R8V5](@T*("`@("`@("`@('TL#0H@("`@("`@("`@
M(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-
M"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E
M<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@
M("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G:6Y6
M97)S:6]N(CH@(C0N-"XQ(BP-"B`@("`@("`@("`B<75E<GDB.B`B+G-H;W<@
M97AT96YT<R!<;GP@<W5M;6%R:7IE('-U;2A/<FEG:6YA;%-I>F4I(BP-"B`@
M("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E
M<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*
M("`@("`@("`@(")R969)9"(Z(")!(BP-"B`@("`@("`@("`B<F5S=6QT1F]R
M;6%T(CH@(G1A8FQE(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1I
M=&QE(CH@(E5N8V]M<')E<W-E9"!$0B!3:7IE(BP-"B`@("`@(")T>7!E(CH@
M(G-T870B#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-
M"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R
M+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB
M#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B
M9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@
M("`@(FUO9&4B.B`B<&%L971T92UC;&%S<VEC(@T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(F-U<W1O;2(Z('L-"B`@("`@("`@("`@(")A>&ES0F]R9&5R
M4VAO=R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X:7-#96YT97)E9%IE<F\B
M.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES0V]L;W)-;V1E(CH@(G1E>'0B
M+`T*("`@("`@("`@("`@(F%X:7-,86)E;"(Z("(B+`T*("`@("`@("`@("`@
M(F%X:7-0;&%C96UE;G0B.B`B875T;R(L#0H@("`@("`@("`@("`B8F%R06QI
M9VYM96YT(CH@,"P-"B`@("`@("`@("`@(")D<F%W4W1Y;&4B.B`B;&EN92(L
M#0H@("`@("`@("`@("`B9FEL;$]P86-I='DB.B`Q,"P-"B`@("`@("`@("`@
M(")G<F%D:65N=$UO9&4B.B`B;F]N92(L#0H@("`@("`@("`@("`B:&ED949R
M;VTB.B![#0H@("`@("`@("`@("`@(")L96=E;F0B.B!F86QS92P-"B`@("`@
M("`@("`@("`@(G1O;VQT:7`B.B!F86QS92P-"B`@("`@("`@("`@("`@(G9I
M>B(Z(&9A;'-E#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(FEN<V5R
M=$YU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B;&EN94EN=&5R<&]L871I
M;VXB.B`B;&EN96%R(BP-"B`@("`@("`@("`@(")L:6YE5VED=&@B.B`Q+`T*
M("`@("`@("`@("`@(G!O:6YT4VEZ92(Z(#4L#0H@("`@("`@("`@("`B<V-A
M;&5$:7-T<FEB=71I;VXB.B![#0H@("`@("`@("`@("`@(")T>7!E(CH@(FQI
M;F5A<B(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<VAO=U!O:6YT
M<R(Z(")N979E<B(L#0H@("`@("`@("`@("`B<W!A;DYU;&QS(CH@9F%L<V4L
M#0H@("`@("`@("`@("`B<W1A8VMI;F<B.B![#0H@("`@("`@("`@("`@(")G
M<F]U<"(Z(")!(BP-"B`@("`@("`@("`@("`@(FUO9&4B.B`B;F]N92(-"B`@
M("`@("`@("`@('TL#0H@("`@("`@("`@("`B=&AR97-H;VQD<U-T>6QE(CH@
M>PT*("`@("`@("`@("`@("`B;6]D92(Z(")O9F8B#0H@("`@("`@("`@("!]
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;72P-"B`@
M("`@("`@("`B=&AR97-H;VQD<R(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@
M(F%B<V]L=71E(BP-"B`@("`@("`@("`@(")S=&5P<R(Z(%L-"B`@("`@("`@
M("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")G<F5E;B(L#0H@
M("`@("`@("`@("`@("`@(G9A;'5E(CH@;G5L;`T*("`@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@
M(G)E9"(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@.#`-"B`@("`@("`@
M("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@("`@("`@
M("`@(G5N:70B.B`B<F5Q<',B#0H@("`@("`@('TL#0H@("`@("`@(")O=F5R
M<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@>PT*("`@
M("`@("`B:"(Z(#@L#0H@("`@("`@(")W(CH@,3(L#0H@("`@("`@(")X(CH@
M,"P-"B`@("`@("`@(GDB.B`X#0H@("`@("!]+`T*("`@("`@(FAI9&54:6UE
M3W9E<G)I9&4B.B!T<G5E+`T*("`@("`@(FED(CH@,3(L#0H@("`@("`B:6YT
M97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@("`@(")L
M96=E;F0B.B![#0H@("`@("`@("`@(F-A;&-S(CH@6UTL#0H@("`@("`@("`@
M(F1I<W!L87E-;V1E(CH@(FQI<W0B+`T*("`@("`@("`@(")P;&%C96UE;G0B
M.B`B8F]T=&]M(BP-"B`@("`@("`@("`B<VAO=TQE9V5N9"(Z('1R=64-"B`@
M("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@("`@(FUO
M9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R="(Z(")N;VYE(@T*("`@
M("`@("!]#0H@("`@("!]+`T*("`@("`@(G1A<F=E=',B.B!;#0H@("`@("`@
M('L-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@
M("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F
M86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@
M("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@("`@("`@("`@?2P-"B`@
M("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")F<F]M(CH@
M>PT*("`@("`@("`@("`@("`B<')O<&5R='DB.B![#0H@("`@("`@("`@("`@
M("`@(FYA;64B.B`B061X;6]N26YG97-T;W)297%U97-T<U)E8V5I=F5D5&]T
M86PB+`T*("`@("`@("`@("`@("`@(")T>7!E(CH@(G-T<FEN9R(-"B`@("`@
M("`@("`@("`@?2P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B<')O<&5R='DB
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@
M("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@
M("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%L-"B`@("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@
M("`@("`B97AP<F5S<VEO;G,B.B!;#0H@("`@("`@("`@("`@("`@("`@('L-
M"B`@("`@("`@("`@("`@("`@("`@("`B;W!E<F%T;W(B.B![#0H@("`@("`@
M("`@("`@("`@("`@("`@("`B;F%M92(Z("(]/2(L#0H@("`@("`@("`@("`@
M("`@("`@("`@("`B=F%L=64B.B`B(@T*("`@("`@("`@("`@("`@("`@("`@
M('TL#0H@("`@("`@("`@("`@("`@("`@("`@(G!R;W!E<G1Y(CH@>PT*("`@
M("`@("`@("`@("`@("`@("`@("`@(FYA;64B.B`B56YD97)L87E.86UE(BP-
M"B`@("`@("`@("`@("`@("`@("`@("`@(")T>7!E(CH@(G-T<FEN9R(-"B`@
M("`@("`@("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@("`@("`@("`@
M(")T>7!E(CH@(F]P97)A=&]R(@T*("`@("`@("`@("`@("`@("`@("!]#0H@
M("`@("`@("`@("`@("`@("!=+`T*("`@("`@("`@("`@("`@("`@(G1Y<&4B
M.B`B;W(B#0H@("`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@("!=+`T*
M("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@
M("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(T+C0N
M,2(L#0H@("`@("`@("`@(G%U97)Y(CH@(D%D>&UO;DEN9V5S=&]R4V%M<&QE
M<U-T;W)E9%1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP
M*5QN?"!W:&5R92!#;'5S=&5R(#T](%PB)$-L=7-T97)<(EQN?"!W:&5R92!0
M;V0@<W1A<G1S=VET:"!<(FEN9V5S=&]R7")<;GP@:6YV;VME('!R;VU?9&5L
M=&$H*5QN?"!S=6UM87)I>F4@<W5M*%9A;'5E*2\H)%]?=&EM94EN=&5R=F%L
M+S%S*2!B>2!B:6XH5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI7&Y\(&]R
M9&5R(&)Y(%1I;65S=&%M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U
M<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*
M("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B
M.B`B02(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE
M<R(-"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T:6UE4VAI9G0B.B`B
M,6TB+`T*("`@("`@(G1I=&QE(CH@(DEN9V5S=&]R(%-A;7!L92!4:')O=6=H
M<'5T(BP-"B`@("`@(")T>7!E(CH@(G1I;65S97)I97,B#0H@("`@?2P-"B`@
M("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B
M9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@
M("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@("`@("!]+`T*("`@("`@
M(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@("`@
M("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B<&%L971T
M92UC;&%S<VEC(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F-U<W1O;2(Z
M('L-"B`@("`@("`@("`@(")A>&ES0F]R9&5R4VAO=R(Z(&9A;'-E+`T*("`@
M("`@("`@("`@(F%X:7-#96YT97)E9%IE<F\B.B!F86QS92P-"B`@("`@("`@
M("`@(")A>&ES0V]L;W)-;V1E(CH@(G1E>'0B+`T*("`@("`@("`@("`@(F%X
M:7-,86)E;"(Z("(B+`T*("`@("`@("`@("`@(F%X:7-0;&%C96UE;G0B.B`B
M875T;R(L#0H@("`@("`@("`@("`B8F%R06QI9VYM96YT(CH@,"P-"B`@("`@
M("`@("`@(")D<F%W4W1Y;&4B.B`B;&EN92(L#0H@("`@("`@("`@("`B9FEL
M;$]P86-I='DB.B`Q,"P-"B`@("`@("`@("`@(")G<F%D:65N=$UO9&4B.B`B
M;F]N92(L#0H@("`@("`@("`@("`B:&ED949R;VTB.B![#0H@("`@("`@("`@
M("`@(")L96=E;F0B.B!F86QS92P-"B`@("`@("`@("`@("`@(G1O;VQT:7`B
M.B!F86QS92P-"B`@("`@("`@("`@("`@(G9I>B(Z(&9A;'-E#0H@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@(FEN<V5R=$YU;&QS(CH@9F%L<V4L#0H@
M("`@("`@("`@("`B;&EN94EN=&5R<&]L871I;VXB.B`B;&EN96%R(BP-"B`@
M("`@("`@("`@(")L:6YE5VED=&@B.B`Q+`T*("`@("`@("`@("`@(G!O:6YT
M4VEZ92(Z(#4L#0H@("`@("`@("`@("`B<V-A;&5$:7-T<FEB=71I;VXB.B![
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(FQI;F5A<B(-"B`@("`@("`@("`@
M('TL#0H@("`@("`@("`@("`B<VAO=U!O:6YT<R(Z(")N979E<B(L#0H@("`@
M("`@("`@("`B<W!A;DYU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B<W1A
M8VMI;F<B.B![#0H@("`@("`@("`@("`@(")G<F]U<"(Z(")!(BP-"B`@("`@
M("`@("`@("`@(FUO9&4B.B`B;F]R;6%L(@T*("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@(")T:')E<VAO;&1S4W1Y;&4B.B![#0H@("`@("`@("`@("`@
M(")M;V1E(CH@(F]F9B(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*
M("`@("`@("`@(")M87!P:6YG<R(Z(%M=+`T*("`@("`@("`@(")T:')E<VAO
M;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B86)S;VQU=&4B+`T*("`@
M("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@("`@
M("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@("`@("`@("`B
M=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@('TL#0H@("`@("`@("`@("`@
M('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B<F5D(BP-"B`@("`@("`@
M("`@("`@("`B=F%L=64B.B`X,`T*("`@("`@("`@("`@("!]#0H@("`@("`@
M("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B=6YI="(Z(")R97%P
M<R(-"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R:61E<R(Z(%M=#0H@("`@
M("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@."P-"B`@
M("`@("`@(G<B.B`Q,BP-"B`@("`@("`@(G@B.B`Q,BP-"B`@("`@("`@(GDB
M.B`X#0H@("`@("!]+`T*("`@("`@(FED(CH@,3$L#0H@("`@("`B:6YT97)V
M86PB.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@("`@(")L96=E
M;F0B.B![#0H@("`@("`@("`@(F-A;&-S(CH@6UTL#0H@("`@("`@("`@(F1I
M<W!L87E-;V1E(CH@(FQI<W0B+`T*("`@("`@("`@(")P;&%C96UE;G0B.B`B
M8F]T=&]M(BP-"B`@("`@("`@("`B<VAO=TQE9V5N9"(Z('1R=64-"B`@("`@
M("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@("`@(FUO9&4B
M.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R="(Z(")N;VYE(@T*("`@("`@
M("!]#0H@("`@("!]+`T*("`@("`@(G1A<F=E=',B.B!;#0H@("`@("`@('L-
M"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@
M(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA
M+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@
M("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@("`@("`@("`@?2P-"B`@("`@
M("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")F<F]M(CH@>PT*
M("`@("`@("`@("`@("`B<')O<&5R='DB.B![#0H@("`@("`@("`@("`@("`@
M(FYA;64B.B`B061X;6]N26YG97-T;W)297%U97-T<U)E8V5I=F5D5&]T86PB
M+`T*("`@("`@("`@("`@("`@(")T>7!E(CH@(G-T<FEN9R(-"B`@("`@("`@
M("`@("`@?2P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B<')O<&5R='DB#0H@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@
M("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B
M='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E
M9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@
M("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S
M:6]N<R(Z(%L-"B`@("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@
M("`B97AP<F5S<VEO;G,B.B!;#0H@("`@("`@("`@("`@("`@("`@('L-"B`@
M("`@("`@("`@("`@("`@("`@("`B;W!E<F%T;W(B.B![#0H@("`@("`@("`@
M("`@("`@("`@("`@("`B;F%M92(Z("(]/2(L#0H@("`@("`@("`@("`@("`@
M("`@("`@("`B=F%L=64B.B`B(@T*("`@("`@("`@("`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`@("`@("`@("`@(G!R;W!E<G1Y(CH@>PT*("`@("`@
M("`@("`@("`@("`@("`@("`@(FYA;64B.B`B56YD97)L87E.86UE(BP-"B`@
M("`@("`@("`@("`@("`@("`@("`@(")T>7!E(CH@(G-T<FEN9R(-"B`@("`@
M("`@("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@("`@("`@("`@(")T
M>7!E(CH@(F]P97)A=&]R(@T*("`@("`@("`@("`@("`@("`@("!]#0H@("`@
M("`@("`@("`@("`@("!=+`T*("`@("`@("`@("`@("`@("`@(G1Y<&4B.B`B
M;W(B#0H@("`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@("!=+`T*("`@
M("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@
M("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(T+C0N,2(L
M#0H@("`@("`@("`@(G%U97)Y(CH@(D%D>&UO;DEN9V5S=&]R4F5Q=65S='-2
M96-E:79E9%1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP
M*5QN?"!W:&5R92!#;'5S=&5R(#T](%PB)$-L=7-T97)<(EQN?"!W:&5R92!#
M;VYT86EN97(@/3T@7")I;F=E<W1O<EPB7&Y\(&EN=F]K92!P<F]M7V1E;'1A
M*"E<;GP@97AT96YD(&-O9&4]=&]S=')I;F<H3&%B96QS+F-O9&4I7&Y\(&5X
M=&5N9"!P871H/71O<W1R:6YG*$QA8F5L<RYP871H*5QN?"!S=6UM87)I>F4@
M079G/7-U;2A686QU92DO*"1?7W1I;65);G1E<G9A;"\Q<RD@8GD@8FEN*%1I
M;65S=&%M<"P@)%]?=&EM94EN=&5R=F%L*2P@8V]D92P@<&%T:%QN?"!O<F1E
M<B!B>2!4:6UE<W1A;7`@87-C+"!C;V1E+"!P871H7&Y\('!R;VIE8W0@5&EM
M97-T86UP+"!C;V1E+"!P871H+"!!=F=<;EQN(BP-"B`@("`@("`@("`B<75E
M<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM1
M3"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R
M969)9"(Z(")!(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?
M<V5R:65S(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1I=&QE(CH@
M(DEN9V5S=&]R(%)E<75E<W1S(&)Y(%!A=&@B+`T*("`@("`@(G1Y<&4B.B`B
M=&EM97-E<FEE<R(-"B`@("!]+`T*("`@('L-"B`@("`@(")D871A<V]U<F-E
M(CH@>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP
M;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U
M<F-E?2(-"B`@("`@('TL#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@
M("`@(")D969A=6QT<R(Z('L-"B`@("`@("`@("`B8V]L;W(B.B![#0H@("`@
M("`@("`@("`B;6]D92(Z(")C;VYT:6YU;W5S+4=R66Q29"(-"B`@("`@("`@
M("!]+`T*("`@("`@("`@(")C=7-T;VTB.B![#0H@("`@("`@("`@("`B87AI
M<T)O<F1E<E-H;W<B.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES0V5N=&5R
M961:97)O(CH@9F%L<V4L#0H@("`@("`@("`@("`B87AI<T-O;&]R36]D92(Z
M(")T97AT(BP-"B`@("`@("`@("`@(")A>&ES3&%B96PB.B`B(BP-"B`@("`@
M("`@("`@(")A>&ES4&QA8V5M96YT(CH@(F%U=&\B+`T*("`@("`@("`@("`@
M(F%X:7-3;V9T36EN(CH@,"P-"B`@("`@("`@("`@(")B87)!;&EG;FUE;G0B
M.B`P+`T*("`@("`@("`@("`@(F1R87=3='EL92(Z(")L:6YE(BP-"B`@("`@
M("`@("`@(")F:6QL3W!A8VET>2(Z(#`L#0H@("`@("`@("`@("`B9W)A9&EE
M;G1-;V1E(CH@(FYO;F4B+`T*("`@("`@("`@("`@(FAI9&5&<F]M(CH@>PT*
M("`@("`@("`@("`@("`B;&5G96YD(CH@9F%L<V4L#0H@("`@("`@("`@("`@
M(")T;V]L=&EP(CH@9F%L<V4L#0H@("`@("`@("`@("`@(")V:7HB.B!F86QS
M90T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")I;G-E<G1.=6QL<R(Z
M(&9A;'-E+`T*("`@("`@("`@("`@(FQI;F5);G1E<G!O;&%T:6]N(CH@(FQI
M;F5A<B(L#0H@("`@("`@("`@("`B;&EN95=I9'1H(CH@,2P-"B`@("`@("`@
M("`@(")P;VEN=%-I>F4B.B`U+`T*("`@("`@("`@("`@(G-C86QE1&ES=')I
M8G5T:6]N(CH@>PT*("`@("`@("`@("`@("`B='EP92(Z(")L:6YE87(B#0H@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G-H;W=0;VEN=',B.B`B;F5V
M97(B+`T*("`@("`@("`@("`@(G-P86Y.=6QL<R(Z(&9A;'-E+`T*("`@("`@
M("`@("`@(G-T86-K:6YG(CH@>PT*("`@("`@("`@("`@("`B9W)O=7`B.B`B
M02(L#0H@("`@("`@("`@("`@(")M;V1E(CH@(FYO;F4B#0H@("`@("`@("`@
M("!]+`T*("`@("`@("`@("`@(G1H<F5S:&]L9'-3='EL92(Z('L-"B`@("`@
M("`@("`@("`@(FUO9&4B.B`B;V9F(@T*("`@("`@("`@("`@?0T*("`@("`@
M("`@('TL#0H@("`@("`@("`@(FUA<'!I;F=S(CH@6UTL#0H@("`@("`@("`@
M(G1H<F5S:&]L9',B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")A8G-O;'5T
M92(L#0H@("`@("`@("`@("`B<W1E<',B.B!;#0H@("`@("`@("`@("`@('L-
M"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@
M("`@("`@(")V86QU92(Z(&YU;&P-"B`@("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")R960B+`T*
M("`@("`@("`@("`@("`@(")V86QU92(Z(#@P#0H@("`@("`@("`@("`@('T-
M"B`@("`@("`@("`@(%T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")U;FET
M(CH@(G,B#0H@("`@("`@('TL#0H@("`@("`@(")O=F5R<FED97,B.B!;70T*
M("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@>PT*("`@("`@("`B:"(Z(#@L
M#0H@("`@("`@(")W(CH@,3(L#0H@("`@("`@(")X(CH@,"P-"B`@("`@("`@
M(GDB.B`Q-@T*("`@("`@?2P-"B`@("`@(")H:61E5&EM94]V97)R:61E(CH@
M=')U92P-"B`@("`@(")I9"(Z(#$V+`T*("`@("`@(FEN=&5R=F%L(CH@(C%M
M(BP-"B`@("`@(")O<'1I;VYS(CH@>PT*("`@("`@("`B;&5G96YD(CH@>PT*
M("`@("`@("`@(")C86QC<R(Z(%M=+`T*("`@("`@("`@(")D:7-P;&%Y36]D
M92(Z(")L:7-T(BP-"B`@("`@("`@("`B<&QA8V5M96YT(CH@(F)O='1O;2(L
M#0H@("`@("`@("`@(G-H;W=,96=E;F0B.B!T<G5E#0H@("`@("`@('TL#0H@
M("`@("`@(")T;V]L=&EP(CH@>PT*("`@("`@("`@(")M;V1E(CH@(G-I;F=L
M92(L#0H@("`@("`@("`@(G-O<G0B.B`B;F]N92(-"B`@("`@("`@?0T*("`@
M("`@?2P-"B`@("`@(")T87)G971S(CH@6PT*("`@("`@("![#0H@("`@("`@
M("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U
M<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD
M871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V5](@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X
M<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9G)O;2(Z('L-"B`@("`@("`@
M("`@("`@(G!R;W!E<G1Y(CH@>PT*("`@("`@("`@("`@("`@(")N86UE(CH@
M(D%D>&UO;DEN9V5S=&]R4F5Q=65S='-296-E:79E9%1O=&%L(BP-"B`@("`@
M("`@("`@("`@("`B='EP92(Z(")S=')I;F<B#0H@("`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(G!R;W!E<G1Y(@T*("`@("`@("`@
M("`@?2P-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@
M("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B
M86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![
M#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@
M("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@
M("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;
M#0H@("`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@("`@(F5X<')E
M<W-I;VYS(CH@6PT*("`@("`@("`@("`@("`@("`@("![#0H@("`@("`@("`@
M("`@("`@("`@("`@(F]P97)A=&]R(CH@>PT*("`@("`@("`@("`@("`@("`@
M("`@("`@(FYA;64B.B`B/3TB+`T*("`@("`@("`@("`@("`@("`@("`@("`@
M(G9A;'5E(CH@(B(-"B`@("`@("`@("`@("`@("`@("`@("!]+`T*("`@("`@
M("`@("`@("`@("`@("`@(")P<F]P97)T>2(Z('L-"B`@("`@("`@("`@("`@
M("`@("`@("`@(")N86UE(CH@(E5N9&5R;&%Y3F%M92(L#0H@("`@("`@("`@
M("`@("`@("`@("`@("`B='EP92(Z(")S=')I;F<B#0H@("`@("`@("`@("`@
M("`@("`@("`@?2P-"B`@("`@("`@("`@("`@("`@("`@("`B='EP92(Z(")O
M<&5R871O<B(-"B`@("`@("`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@
M("`@("`@72P-"B`@("`@("`@("`@("`@("`@(")T>7!E(CH@(F]R(@T*("`@
M("`@("`@("`@("`@('T-"B`@("`@("`@("`@("`@72P-"B`@("`@("`@("`@
M("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL
M#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-"XT+C$B+`T*("`@("`@
M("`@(")Q=65R>2(Z(")!9'AM;VY);F=E<W1O<E=A;%-E9VUE;G1S36%X06=E
M4V5C;VYD<UQN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@
M=VAE<F4@0VQU<W1E<B`]/2!<(B1#;'5S=&5R7")<;GP@=VAE<F4@0V]N=&%I
M;F5R(#T](%PB:6YG97-T;W)<(EQN?"!W:&5R92!686QU92`A/2`Y,C(S,S<R
M,#,V+C@U-#<W-C1<;GP@;6%K92US97)I97,@5F%L=64];6%X*%9A;'5E*2!O
M;B!4:6UE<W1A;7`@9G)O;2`D7U]T:6UE1G)O;2!T;R`D7U]T:6UE5&\@<W1E
M<"`D7U]T:6UE26YT97)V86P@8GD@4&]D7&Y\(&5X=&5N9"!S=&%T<SUS97)I
M97-?<W1A='-?9'EN86UI8RA686QU92E<;GP@;W)D97(@8GD@=&]R96%L*'-T
M871S+FUA>"D@9&5S8UQN?"!L:6UI="`Q,%QN?"!P<F]J96-T(%!O9"P@5&EM
M97-T86UP+"!686QU95QN(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B
M<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@
M("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")!(BP-
M"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?<V5R:65S7V%D>%]S
M97)I97,B#0H@("`@("`@('T-"B`@("`@(%TL#0H@("`@("`B=&EM95-H:69T
M(CH@(C%M(BP-"B`@("`@(")T:71L92(Z(")4;W`@36%X(%-E9VUE;G0@06=E
M(BP-"B`@("`@(")T>7!E(CH@(G1I;65S97)I97,B#0H@("`@?2P-"B`@("![
M#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A
M9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@
M("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@("`@("!]+`T*("`@("`@(F9I
M96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@("`@("`@
M("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B<&%L971T92UC
M;&%S<VEC(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F-U<W1O;2(Z('L-
M"B`@("`@("`@("`@(")A>&ES0F]R9&5R4VAO=R(Z(&9A;'-E+`T*("`@("`@
M("`@("`@(F%X:7-#96YT97)E9%IE<F\B.B!F86QS92P-"B`@("`@("`@("`@
M(")A>&ES0V]L;W)-;V1E(CH@(G1E>'0B+`T*("`@("`@("`@("`@(F%X:7-,
M86)E;"(Z("(B+`T*("`@("`@("`@("`@(F%X:7-0;&%C96UE;G0B.B`B875T
M;R(L#0H@("`@("`@("`@("`B8F%R06QI9VYM96YT(CH@,"P-"B`@("`@("`@
M("`@(")D<F%W4W1Y;&4B.B`B;&EN92(L#0H@("`@("`@("`@("`B9FEL;$]P
M86-I='DB.B`Q,"P-"B`@("`@("`@("`@(")G<F%D:65N=$UO9&4B.B`B;F]N
M92(L#0H@("`@("`@("`@("`B:&ED949R;VTB.B![#0H@("`@("`@("`@("`@
M(")L96=E;F0B.B!F86QS92P-"B`@("`@("`@("`@("`@(G1O;VQT:7`B.B!F
M86QS92P-"B`@("`@("`@("`@("`@(G9I>B(Z(&9A;'-E#0H@("`@("`@("`@
M("!]+`T*("`@("`@("`@("`@(FEN<V5R=$YU;&QS(CH@9F%L<V4L#0H@("`@
M("`@("`@("`B;&EN94EN=&5R<&]L871I;VXB.B`B;&EN96%R(BP-"B`@("`@
M("`@("`@(")L:6YE5VED=&@B.B`Q+`T*("`@("`@("`@("`@(G!O:6YT4VEZ
M92(Z(#4L#0H@("`@("`@("`@("`B<V-A;&5$:7-T<FEB=71I;VXB.B![#0H@
M("`@("`@("`@("`@(")T>7!E(CH@(FQI;F5A<B(-"B`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`B<VAO=U!O:6YT<R(Z(")N979E<B(L#0H@("`@("`@
M("`@("`B<W!A;DYU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B<W1A8VMI
M;F<B.B![#0H@("`@("`@("`@("`@(")G<F]U<"(Z(")!(BP-"B`@("`@("`@
M("`@("`@(FUO9&4B.B`B;F]N92(-"B`@("`@("`@("`@('TL#0H@("`@("`@
M("`@("`B=&AR97-H;VQD<U-T>6QE(CH@>PT*("`@("`@("`@("`@("`B;6]D
M92(Z(")O9F8B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@
M("`@("`B;6%P<&EN9W,B.B!;72P-"B`@("`@("`@("`B=&AR97-H;VQD<R(Z
M('L-"B`@("`@("`@("`@(")M;V1E(CH@(F%B<V]L=71E(BP-"B`@("`@("`@
M("`@(")S=&5P<R(Z(%L-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@
M("`@(")C;VQO<B(Z(")G<F5E;B(L#0H@("`@("`@("`@("`@("`@(G9A;'5E
M(CH@;G5L;`T*("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@("![#0H@
M("`@("`@("`@("`@("`@(F-O;&]R(CH@(G)E9"(L#0H@("`@("`@("`@("`@
M("`@(G9A;'5E(CH@.#`-"B`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@
M70T*("`@("`@("`@('TL#0H@("`@("`@("`@(G5N:70B.B`B<F5Q<',B#0H@
M("`@("`@('TL#0H@("`@("`@(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-
M"B`@("`@(")G<FED4&]S(CH@>PT*("`@("`@("`B:"(Z(#@L#0H@("`@("`@
M(")W(CH@,3(L#0H@("`@("`@(")X(CH@,3(L#0H@("`@("`@(")Y(CH@,38-
M"B`@("`@('TL#0H@("`@("`B:60B.B`R,BP-"B`@("`@(")I;G1E<G9A;"(Z
M("(Q;2(L#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(FQE9V5N9"(Z
M('L-"B`@("`@("`@("`B8V%L8W,B.B!;72P-"B`@("`@("`@("`B9&ES<&QA
M>4UO9&4B.B`B;&ES="(L#0H@("`@("`@("`@(G!L86-E;65N="(Z(")B;W1T
M;VTB+`T*("`@("`@("`@(")S:&]W3&5G96YD(CH@=')U90T*("`@("`@("!]
M+`T*("`@("`@("`B=&]O;'1I<"(Z('L-"B`@("`@("`@("`B;6]D92(Z(")S
M:6YG;&4B+`T*("`@("`@("`@(")S;W)T(CH@(FYO;F4B#0H@("`@("`@('T-
M"B`@("`@('TL#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@
M("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T
M87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U
M:60B.B`B)'M$871A<V]U<F-E?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@
M(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F9R;VTB.B![#0H@("`@
M("`@("`@("`@(")P<F]P97)T>2(Z('L-"B`@("`@("`@("`@("`@("`B;F%M
M92(Z(")!9'AM;VY);F=E<W1O<E)E<75E<W1S4F5C96EV9614;W1A;"(L#0H@
M("`@("`@("`@("`@("`@(G1Y<&4B.B`B<W1R:6YG(@T*("`@("`@("`@("`@
M("!]+`T*("`@("`@("`@("`@("`B='EP92(Z(")P<F]P97)T>2(-"B`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@
M("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E
M(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E
M(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@
M("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS
M(CH@6PT*("`@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`@(")E
M>'!R97-S:6]N<R(Z(%L-"B`@("`@("`@("`@("`@("`@("`@>PT*("`@("`@
M("`@("`@("`@("`@("`@(")O<&5R871O<B(Z('L-"B`@("`@("`@("`@("`@
M("`@("`@("`@(")N86UE(CH@(CT](BP-"B`@("`@("`@("`@("`@("`@("`@
M("`@(")V86QU92(Z("(B#0H@("`@("`@("`@("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@("`@("`@("`@("`B<')O<&5R='DB.B![#0H@("`@("`@("`@
M("`@("`@("`@("`@("`B;F%M92(Z(")5;F1E<FQA>4YA;64B+`T*("`@("`@
M("`@("`@("`@("`@("`@("`@(G1Y<&4B.B`B<W1R:6YG(@T*("`@("`@("`@
M("`@("`@("`@("`@('TL#0H@("`@("`@("`@("`@("`@("`@("`@(G1Y<&4B
M.B`B;W!E<F%T;W(B#0H@("`@("`@("`@("`@("`@("`@('T-"B`@("`@("`@
M("`@("`@("`@(%TL#0H@("`@("`@("`@("`@("`@("`B='EP92(Z(")O<B(-
M"B`@("`@("`@("`@("`@("!]#0H@("`@("`@("`@("`@(%TL#0H@("`@("`@
M("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@
M("!]+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C0N-"XQ(BP-"B`@
M("`@("`@("`B<75E<GDB.B`B061X;6]N26YG97-T;W)297%U97-T<U)E8V5I
M=F5D5&]T86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\
M('=H97)E($-L=7-T97(@/3T@7"(D0VQU<W1E<EPB7&Y\('=H97)E($-O;G1A
M:6YE<B`]/2!<(FEN9V5S=&]R7")<;GP@:6YV;VME('!R;VU?9&5L=&$H*5QN
M?"!E>'1E;F0@8V]D93UT;W-T<FEN9RA,86)E;',N8V]D92E<;GP@<W5M;6%R
M:7IE($%V9SUS=6TH5F%L=64I+R@D7U]T:6UE26YT97)V86PO,7,I(&)Y(&)I
M;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL($AO<W1<;GP@;W)D97(@
M8GD@5&EM97-T86UP(&%S8RP@2&]S=%QN?"!P<F]J96-T(%1I;65S=&%M<"P@
M2&]S="P@079G7&Y<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A
M=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@
M(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@
M("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@
M("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z("));F=E<W1O<B!297%U
M97-T<R!">2!(;W-T(BP-"B`@("`@(")T>7!E(CH@(G1I;65S97)I97,B#0H@
M("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@
M(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R
M8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@("`@("!]
M+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B
M.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B
M.B`B<&%L971T92UC;&%S<VEC(@T*("`@("`@("`@('TL#0H@("`@("`@("`@
M(F-U<W1O;2(Z('L-"B`@("`@("`@("`@(")A>&ES0F]R9&5R4VAO=R(Z(&9A
M;'-E+`T*("`@("`@("`@("`@(F%X:7-#96YT97)E9%IE<F\B.B!F86QS92P-
M"B`@("`@("`@("`@(")A>&ES0V]L;W)-;V1E(CH@(G1E>'0B+`T*("`@("`@
M("`@("`@(F%X:7-,86)E;"(Z("(B+`T*("`@("`@("`@("`@(F%X:7-0;&%C
M96UE;G0B.B`B875T;R(L#0H@("`@("`@("`@("`B87AI<U-O9G1-:6XB.B`P
M+`T*("`@("`@("`@("`@(F)A<D%L:6=N;65N="(Z("TQ+`T*("`@("`@("`@
M("`@(F1R87=3='EL92(Z(")L:6YE(BP-"B`@("`@("`@("`@(")F:6QL3W!A
M8VET>2(Z(#`L#0H@("`@("`@("`@("`B9W)A9&EE;G1-;V1E(CH@(FYO;F4B
M+`T*("`@("`@("`@("`@(FAI9&5&<F]M(CH@>PT*("`@("`@("`@("`@("`B
M;&5G96YD(CH@9F%L<V4L#0H@("`@("`@("`@("`@(")T;V]L=&EP(CH@9F%L
M<V4L#0H@("`@("`@("`@("`@(")V:7HB.B!F86QS90T*("`@("`@("`@("`@
M?2P-"B`@("`@("`@("`@(")I;G-E<G1.=6QL<R(Z(&9A;'-E+`T*("`@("`@
M("`@("`@(FQI;F5);G1E<G!O;&%T:6]N(CH@(FQI;F5A<B(L#0H@("`@("`@
M("`@("`B;&EN95=I9'1H(CH@,2P-"B`@("`@("`@("`@(")P;VEN=%-I>F4B
M.B`P+`T*("`@("`@("`@("`@(G-C86QE1&ES=')I8G5T:6]N(CH@>PT*("`@
M("`@("`@("`@("`B='EP92(Z(")L:6YE87(B#0H@("`@("`@("`@("!]+`T*
M("`@("`@("`@("`@(G-H;W=0;VEN=',B.B`B875T;R(L#0H@("`@("`@("`@
M("`B<W!A;DYU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B<W1A8VMI;F<B
M.B![#0H@("`@("`@("`@("`@(")G<F]U<"(Z(")!(BP-"B`@("`@("`@("`@
M("`@(FUO9&4B.B`B;F]N92(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`B=&AR97-H;VQD<U-T>6QE(CH@>PT*("`@("`@("`@("`@("`B;6]D92(Z
M(")O9F8B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@
M("`B;6%P<&EN9W,B.B!;72P-"B`@("`@("`@("`B=&AR97-H;VQD<R(Z('L-
M"B`@("`@("`@("`@(")M;V1E(CH@(F%B<V]L=71E(BP-"B`@("`@("`@("`@
M(")S=&5P<R(Z(%L-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@
M(")C;VQO<B(Z(")G<F5E;B(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@
M;G5L;`T*("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@("![#0H@("`@
M("`@("`@("`@("`@(F-O;&]R(CH@(G)E9"(L#0H@("`@("`@("`@("`@("`@
M(G9A;'5E(CH@.#`-"B`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*
M("`@("`@("`@('TL#0H@("`@("`@("`@(G5N:70B.B`B;F]N92(-"B`@("`@
M("`@?2P-"B`@("`@("`@(F]V97)R:61E<R(Z(%M=#0H@("`@("!]+`T*("`@
M("`@(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@."P-"B`@("`@("`@(G<B
M.B`Q,BP-"B`@("`@("`@(G@B.B`P+`T*("`@("`@("`B>2(Z(#(T#0H@("`@
M("!]+`T*("`@("`@(FED(CH@,C,L#0H@("`@("`B:6YT97)V86PB.B`B,6TB
M+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@("`@(")L96=E;F0B.B![#0H@
M("`@("`@("`@(F-A;&-S(CH@6UTL#0H@("`@("`@("`@(F1I<W!L87E-;V1E
M(CH@(FQI<W0B+`T*("`@("`@("`@(")P;&%C96UE;G0B.B`B8F]T=&]M(BP-
M"B`@("`@("`@("`B<VAO=TQE9V5N9"(Z('1R=64-"B`@("`@("`@?2P-"B`@
M("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@("`@(FUO9&4B.B`B<VEN9VQE
M(BP-"B`@("`@("`@("`B<V]R="(Z(")N;VYE(@T*("`@("`@("!]#0H@("`@
M("!]+`T*("`@("`@(G1A<F=E=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@
M("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R
M8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A
M=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@
M(B1[1&%T87-O=7)C97TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP
M<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")F<F]M(CH@>PT*("`@("`@("`@
M("`@("`B<')O<&5R='DB.B![#0H@("`@("`@("`@("`@("`@(FYA;64B.B`B
M061X;6]N26YG97-T;W)297%U97-T<U)E8V5I=F5D5&]T86PB+`T*("`@("`@
M("`@("`@("`@(")T>7!E(CH@(G-T<FEN9R(-"B`@("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@("`@(G1Y<&4B.B`B<')O<&5R='DB#0H@("`@("`@("`@
M("!]+`T*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@
M(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A
M;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-
M"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@
M("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%L-
M"B`@("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@("`B97AP<F5S
M<VEO;G,B.B!;#0H@("`@("`@("`@("`@("`@("`@('L-"B`@("`@("`@("`@
M("`@("`@("`@("`B;W!E<F%T;W(B.B![#0H@("`@("`@("`@("`@("`@("`@
M("`@("`B;F%M92(Z("(]/2(L#0H@("`@("`@("`@("`@("`@("`@("`@("`B
M=F%L=64B.B`B(@T*("`@("`@("`@("`@("`@("`@("`@('TL#0H@("`@("`@
M("`@("`@("`@("`@("`@(G!R;W!E<G1Y(CH@>PT*("`@("`@("`@("`@("`@
M("`@("`@("`@(FYA;64B.B`B56YD97)L87E.86UE(BP-"B`@("`@("`@("`@
M("`@("`@("`@("`@(")T>7!E(CH@(G-T<FEN9R(-"B`@("`@("`@("`@("`@
M("`@("`@("!]+`T*("`@("`@("`@("`@("`@("`@("`@(")T>7!E(CH@(F]P
M97)A=&]R(@T*("`@("`@("`@("`@("`@("`@("!]#0H@("`@("`@("`@("`@
M("`@("!=+`T*("`@("`@("`@("`@("`@("`@(G1Y<&4B.B`B;W(B#0H@("`@
M("`@("`@("`@("`@?0T*("`@("`@("`@("`@("!=+`T*("`@("`@("`@("`@
M("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-
M"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(T+C0N,2(L#0H@("`@("`@
M("`@(G%U97)Y(CH@(D%D>&UO;DEN9V5S=&]R5V%L4V5G;65N='-#;W5N=%QN
M?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU
M<W1E<B`]/2!<(B1#;'5S=&5R7")<;GP@=VAE<F4@0V]N=&%I;F5R(#T](%PB
M:6YG97-T;W)<(EQN?"!E>'1E;F0@;65T<FEC/71O<W1R:6YG*$QA8F5L<RYM
M971R:6,I7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I(&)Y(&)I;BA4
M:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL($AO<W0L(&UE=')I8UQN?"!M
M86ME+7-E<FEE<R!686QU93US=6TH5F%L=64I(&]N(%1I;65S=&%M<"!F<F]M
M("1?7W1I;65&<F]M('1O("1?7W1I;654;R!S=&5P("1?7W1I;65);G1E<G9A
M;"!B>2!(;W-T7&Y\(&5X=&5N9"!S=&%T<SUS97)I97-?<W1A='-?9'EN86UI
M8RA686QU92E<;GP@;W)D97(@8GD@=&]R96%L*'-T871S+FUA>"D@9&5S8UQN
M?"!L:6UI="`Q,%QN?"!P<F]J96-T($AO<W0L(%1I;65S=&%M<"P@5F%L=64B
M+`T*("`@("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@
M(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R
M=64L#0H@("`@("`@("`@(G)E9DED(CH@(D$B+`T*("`@("`@("`@(")R97-U
M;'1&;W)M870B.B`B=&EM95]S97)I97-?861X7W-E<FEE<R(-"B`@("`@("`@
M?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z(")4;W`@5T%,(%-E9VUE;G1S
M($-O=6YT(BP-"B`@("`@(")T>7!E(CH@(G1I;65S97)I97,B#0H@("`@?2P-
M"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B
M.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*
M("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@("`@("!]+`T*("`@
M("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@
M("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B<&%L
M971T92UC;&%S<VEC(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F-U<W1O
M;2(Z('L-"B`@("`@("`@("`@(")A>&ES0F]R9&5R4VAO=R(Z(&9A;'-E+`T*
M("`@("`@("`@("`@(F%X:7-#96YT97)E9%IE<F\B.B!F86QS92P-"B`@("`@
M("`@("`@(")A>&ES0V]L;W)-;V1E(CH@(G1E>'0B+`T*("`@("`@("`@("`@
M(F%X:7-,86)E;"(Z("(B+`T*("`@("`@("`@("`@(F%X:7-0;&%C96UE;G0B
M.B`B875T;R(L#0H@("`@("`@("`@("`B8F%R06QI9VYM96YT(CH@,"P-"B`@
M("`@("`@("`@(")D<F%W4W1Y;&4B.B`B;&EN92(L#0H@("`@("`@("`@("`B
M9FEL;$]P86-I='DB.B`P+`T*("`@("`@("`@("`@(F=R861I96YT36]D92(Z
M(")N;VYE(BP-"B`@("`@("`@("`@(")H:61E1G)O;2(Z('L-"B`@("`@("`@
M("`@("`@(FQE9V5N9"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=&]O;'1I
M<"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=FEZ(CH@9F%L<V4-"B`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`B:6YS97)T3G5L;',B.B!F86QS92P-
M"B`@("`@("`@("`@(")L:6YE26YT97)P;VQA=&EO;B(Z(")L:6YE87(B+`T*
M("`@("`@("`@("`@(FQI;F57:61T:"(Z(#$L#0H@("`@("`@("`@("`B<&]I
M;G13:7IE(CH@-2P-"B`@("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z
M('L-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@
M("`@?2P-"B`@("`@("`@("`@(")S:&]W4&]I;G1S(CH@(FYE=F5R(BP-"B`@
M("`@("`@("`@(")S<&%N3G5L;',B.B!F86QS92P-"B`@("`@("`@("`@(")S
M=&%C:VEN9R(Z('L-"B`@("`@("`@("`@("`@(F=R;W5P(CH@(D$B+`T*("`@
M("`@("`@("`@("`B;6]D92(Z(")N;W)M86PB#0H@("`@("`@("`@("!]+`T*
M("`@("`@("`@("`@(G1H<F5S:&]L9'-3='EL92(Z('L-"B`@("`@("`@("`@
M("`@(FUO9&4B.B`B;V9F(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL
M#0H@("`@("`@("`@(FUA<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S
M:&]L9',B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@
M("`@("`@("`@("`B<W1E<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@
M("`@("`@("`@("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@
M(")V86QU92(Z(&YU;&P-"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@
M("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@
M("`@("`@("`@(")V86QU92(Z(#@P#0H@("`@("`@("`@("`@('T-"B`@("`@
M("`@("`@(%T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")U;FET(CH@(G-H
M;W)T(@T*("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6UT-"B`@
M("`@('TL#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`X+`T*
M("`@("`@("`B=R(Z(#$R+`T*("`@("`@("`B>"(Z(#$R+`T*("`@("`@("`B
M>2(Z(#(T#0H@("`@("!]+`T*("`@("`@(FAI9&54:6UE3W9E<G)I9&4B.B!T
M<G5E+`T*("`@("`@(FED(CH@,3@L#0H@("`@("`B:6YT97)V86PB.B`B,6TB
M+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@("`@(")L96=E;F0B.B![#0H@
M("`@("`@("`@(F-A;&-S(CH@6UTL#0H@("`@("`@("`@(F1I<W!L87E-;V1E
M(CH@(FQI<W0B+`T*("`@("`@("`@(")P;&%C96UE;G0B.B`B8F]T=&]M(BP-
M"B`@("`@("`@("`B<VAO=TQE9V5N9"(Z('1R=64-"B`@("`@("`@?2P-"B`@
M("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@("`@(FUO9&4B.B`B;75L=&DB
M+`T*("`@("`@("`@(")S;W)T(CH@(F1E<V,B#0H@("`@("`@('T-"B`@("`@
M('TL#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@("`@
M(")D871A8F%S92(Z("(D1&%T86)A<V4B+`T*("`@("`@("`@(")D871A<V]U
M<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD
M871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V5](@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X
M<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@
M("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T
M>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D
M=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@
M("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I
M;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@
M("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")H:61E(CH@9F%L
M<V4L#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-"XT+C$B+`T*("`@
M("`@("`@(")Q=65R>2(Z(")!9'AM;VY);F=E<W1O<E1A8FQE0V%R9&EN86QI
M='E#;W5N=%QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@
M97AT96YD('1A8FQE(#T@=&]S=')I;F<H3&%B96QS+G1A8FQE*5QN?"!M86ME
M+7-E<FEE<R!686QU93UA=F<H5F%L=64I(&1E9F%U;'0@/2`P(&]N(%1I;65S
M=&%M<"!F<F]M("1?7W1I;65&<F]M('1O("1?7W1I;654;R!S=&5P("1?7W1I
M;65);G1E<G9A;"!B>2!T86)L95QN?"!E>'1E;F0@<W1A=',]<V5R:65S7W-T
M871S7V1Y;F%M:6,H5F%L=64I7&Y\(&]R9&5R(&)Y('1O:6YT*'-T871S+FUA
M>"D@9&5S8UQN?"!P<F]J96-T(%1I;65S=&%M<"P@=&%B;&4L(%9A;'5E7&Y\
M(&QI;6ET(#$P(%QN?"!E>'1E;F0@5F%L=64]<V5R:65S7V9I;&Q?9F]R=V%R
M9"A686QU92P@,"E<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A
M=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@
M(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@
M("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<U]A9'A?<V5R
M:65S(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1I;653:&EF="(Z
M("(Q;2(L#0H@("`@("`B=&ET;&4B.B`B5&]P($%C=&EV92!397)I97,B+`T*
M("`@("`@(G1Y<&4B.B`B=&EM97-E<FEE<R(-"B`@("!]+`T*("`@('L-"B`@
M("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA
M+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U
M:60B.B`B)'M$871A<V]U<F-E?2(-"B`@("`@('TL#0H@("`@("`B9FEE;&1#
M;VYF:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z('L-"B`@("`@("`@("`B
M8V]L;W(B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")P86QE='1E+6-L87-S
M:6,B#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B8W5S=&]M(CH@>PT*("`@
M("`@("`@("`@(F%X:7-";W)D97)3:&]W(CH@9F%L<V4L#0H@("`@("`@("`@
M("`B87AI<T-E;G1E<F5D6F5R;R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X
M:7-#;VQO<DUO9&4B.B`B=&5X="(L#0H@("`@("`@("`@("`B87AI<TQA8F5L
M(CH@(B(L#0H@("`@("`@("`@("`B87AI<U!L86-E;65N="(Z(")A=71O(BP-
M"B`@("`@("`@("`@(")A>&ES4V]F=$UI;B(Z(#`L#0H@("`@("`@("`@("`B
M8F%R06QI9VYM96YT(CH@+3$L#0H@("`@("`@("`@("`B9')A=U-T>6QE(CH@
M(FQI;F4B+`T*("`@("`@("`@("`@(F9I;&Q/<&%C:71Y(CH@,"P-"B`@("`@
M("`@("`@(")G<F%D:65N=$UO9&4B.B`B;F]N92(L#0H@("`@("`@("`@("`B
M:&ED949R;VTB.B![#0H@("`@("`@("`@("`@(")L96=E;F0B.B!F86QS92P-
M"B`@("`@("`@("`@("`@(G1O;VQT:7`B.B!F86QS92P-"B`@("`@("`@("`@
M("`@(G9I>B(Z(&9A;'-E#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(FEN<V5R=$YU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B;&EN94EN=&5R
M<&]L871I;VXB.B`B;&EN96%R(BP-"B`@("`@("`@("`@(")L:6YE5VED=&@B
M.B`Q+`T*("`@("`@("`@("`@(G!O:6YT4VEZ92(Z(#`L#0H@("`@("`@("`@
M("`B<V-A;&5$:7-T<FEB=71I;VXB.B![#0H@("`@("`@("`@("`@(")T>7!E
M(CH@(FQI;F5A<B(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<VAO
M=U!O:6YT<R(Z(")A=71O(BP-"B`@("`@("`@("`@(")S<&%N3G5L;',B.B!F
M86QS92P-"B`@("`@("`@("`@(")S=&%C:VEN9R(Z('L-"B`@("`@("`@("`@
M("`@(F=R;W5P(CH@(D$B+`T*("`@("`@("`@("`@("`B;6]D92(Z(")N;VYE
M(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")T:')E<VAO;&1S4W1Y
M;&4B.B![#0H@("`@("`@("`@("`@(")M;V1E(CH@(F]F9B(-"B`@("`@("`@
M("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")M87!P:6YG<R(Z(%M=
M+`T*("`@("`@("`@(")T:')E<VAO;&1S(CH@>PT*("`@("`@("`@("`@(FUO
M9&4B.B`B86)S;VQU=&4B+`T*("`@("`@("`@("`@(G-T97!S(CH@6PT*("`@
M("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(F=R965N
M(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@("`@
M("`@('TL#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L
M;W(B.B`B<F5D(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*("`@
M("`@("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@
M("`@("`@("`B=6YI="(Z(")B>71E<R(-"B`@("`@("`@?2P-"B`@("`@("`@
M(F]V97)R:61E<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![
M#0H@("`@("`@(")H(CH@."P-"B`@("`@("`@(G<B.B`Q,BP-"B`@("`@("`@
M(G@B.B`P+`T*("`@("`@("`B>2(Z(#,R#0H@("`@("!]+`T*("`@("`@(FED
M(CH@,C4L#0H@("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO
M;G,B.B![#0H@("`@("`@(")L96=E;F0B.B![#0H@("`@("`@("`@(F-A;&-S
M(CH@6UTL#0H@("`@("`@("`@(F1I<W!L87E-;V1E(CH@(FQI<W0B+`T*("`@
M("`@("`@(")P;&%C96UE;G0B.B`B8F]T=&]M(BP-"B`@("`@("`@("`B<VAO
M=TQE9V5N9"(Z('1R=64-"B`@("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B
M.B![#0H@("`@("`@("`@(FUO9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B
M<V]R="(Z(")N;VYE(@T*("`@("`@("!]#0H@("`@("!]+`T*("`@("`@(G1A
M<F=E=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B9&%T86)A<V4B.B`B
M365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@
M("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T
M87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@
M("`@("`@("`@(")F<F]M(CH@>PT*("`@("`@("`@("`@("`B<')O<&5R='DB
M.B![#0H@("`@("`@("`@("`@("`@(FYA;64B.B`B061X;6]N26YG97-T;W)2
M97%U97-T<U)E8V5I=F5D5&]T86PB+`T*("`@("`@("`@("`@("`@(")T>7!E
M(CH@(G-T<FEN9R(-"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@
M(G1Y<&4B.B`B<')O<&5R='DB#0H@("`@("`@("`@("!]+`T*("`@("`@("`@
M("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z
M(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@
M("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@
M(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N
M9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@
M("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%L-"B`@("`@("`@("`@("`@
M("![#0H@("`@("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;#0H@("`@
M("`@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`@("`@("`B;W!E
M<F%T;W(B.B![#0H@("`@("`@("`@("`@("`@("`@("`@("`B;F%M92(Z("(]
M/2(L#0H@("`@("`@("`@("`@("`@("`@("`@("`B=F%L=64B.B`B(@T*("`@
M("`@("`@("`@("`@("`@("`@('TL#0H@("`@("`@("`@("`@("`@("`@("`@
M(G!R;W!E<G1Y(CH@>PT*("`@("`@("`@("`@("`@("`@("`@("`@(FYA;64B
M.B`B56YD97)L87E.86UE(BP-"B`@("`@("`@("`@("`@("`@("`@("`@(")T
M>7!E(CH@(G-T<FEN9R(-"B`@("`@("`@("`@("`@("`@("`@("!]+`T*("`@
M("`@("`@("`@("`@("`@("`@(")T>7!E(CH@(F]P97)A=&]R(@T*("`@("`@
M("`@("`@("`@("`@("!]#0H@("`@("`@("`@("`@("`@("!=+`T*("`@("`@
M("`@("`@("`@("`@(G1Y<&4B.B`B;W(B#0H@("`@("`@("`@("`@("`@?0T*
M("`@("`@("`@("`@("!=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B
M#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU
M9VEN5F5R<VEO;B(Z("(T+C0N,2(L#0H@("`@("`@("`@(G%U97)Y(CH@(D%D
M>&UO;DEN9V5S=&]R5V%L4V5G;65N='-3:7IE0GET97-<;GP@=VAE<F4@)%]?
M=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T97(@/3T@7"(D
M0VQU<W1E<EPB7&Y\('=H97)E($-O;G1A:6YE<B`]/2!<(FEN9V5S=&]R7")<
M;GP@97AT96YD(&UE=')I8SUT;W-T<FEN9RA,86)E;',N;65T<FEC*5QN?"!S
M=6UM87)I>F4@:&EN="YS:'5F9FQE:V5Y/4AO<W0@5F%L=64]879G*%9A;'5E
M*2!B>2!B:6XH5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI+"!M971R:6,L
M($AO<W1<;GP@;6%K92US97)I97,@5F%L=64]<W5M*%9A;'5E*2!D969A=6QT
M(#T@,"!O;B!4:6UE<W1A;7`@9G)O;2`D7U]T:6UE1G)O;2!T;R`D7U]T:6UE
M5&\@<W1E<"`D7U]T:6UE26YT97)V86P@8GD@2&]S=%QN?"!E>'1E;F0@<W1A
M=',]<V5R:65S7W-T871S7V1Y;F%M:6,H5F%L=64I7&Y\(&]R9&5R(&)Y('1O
M<F5A;"AS=&%T<RYM87@I(&1E<V-<;GP@;&EM:70@,3!<;GP@<')O:F5C="!(
M;W-T+"!4:6UE<W1A;7`L(%9A;'5E7&Y<;B(L#0H@("`@("`@("`@(G%U97)Y
M4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB
M+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F
M260B.B`B02(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E
M<FEE<U]A9'A?<V5R:65S(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@
M(G1I=&QE(CH@(E1O<"!704P@4V5G;65N=',@1&ES:R!5<V%G92(L#0H@("`@
M("`B='EP92(Z(")T:6UE<V5R:65S(@T*("`@('TL#0H@("`@>PT*("`@("`@
M(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V5](@T*("`@("`@?2P-"B`@("`@(")F:65L9$-O;F9I
M9R(Z('L-"B`@("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@(")C;VQO
M<B(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@(G!A;&5T=&4M8VQA<W-I8R(-
M"B`@("`@("`@("!]+`T*("`@("`@("`@(")C=7-T;VTB.B![#0H@("`@("`@
M("`@("`B87AI<T)O<F1E<E-H;W<B.B!F86QS92P-"B`@("`@("`@("`@(")A
M>&ES0V5N=&5R961:97)O(CH@9F%L<V4L#0H@("`@("`@("`@("`B87AI<T-O
M;&]R36]D92(Z(")T97AT(BP-"B`@("`@("`@("`@(")A>&ES3&%B96PB.B`B
M(BP-"B`@("`@("`@("`@(")A>&ES4&QA8V5M96YT(CH@(F%U=&\B+`T*("`@
M("`@("`@("`@(F%X:7-3;V9T36EN(CH@,"P-"B`@("`@("`@("`@(")B87)!
M;&EG;FUE;G0B.B`M,2P-"B`@("`@("`@("`@(")D<F%W4W1Y;&4B.B`B;&EN
M92(L#0H@("`@("`@("`@("`B9FEL;$]P86-I='DB.B`P+`T*("`@("`@("`@
M("`@(F=R861I96YT36]D92(Z(")N;VYE(BP-"B`@("`@("`@("`@(")H:61E
M1G)O;2(Z('L-"B`@("`@("`@("`@("`@(FQE9V5N9"(Z(&9A;'-E+`T*("`@
M("`@("`@("`@("`B=&]O;'1I<"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B
M=FEZ(CH@9F%L<V4-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B:6YS
M97)T3G5L;',B.B!F86QS92P-"B`@("`@("`@("`@(")L:6YE26YT97)P;VQA
M=&EO;B(Z(")L:6YE87(B+`T*("`@("`@("`@("`@(FQI;F57:61T:"(Z(#$L
M#0H@("`@("`@("`@("`B<&]I;G13:7IE(CH@,"P-"B`@("`@("`@("`@(")S
M8V%L941I<W1R:6)U=&EO;B(Z('L-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B
M;&EN96%R(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")S:&]W4&]I
M;G1S(CH@(F%U=&\B+`T*("`@("`@("`@("`@(G-P86Y.=6QL<R(Z(&9A;'-E
M+`T*("`@("`@("`@("`@(G-T86-K:6YG(CH@>PT*("`@("`@("`@("`@("`B
M9W)O=7`B.B`B02(L#0H@("`@("`@("`@("`@(")M;V1E(CH@(FYO;F4B#0H@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G1H<F5S:&]L9'-3='EL92(Z
M('L-"B`@("`@("`@("`@("`@(FUO9&4B.B`B9&%S:&5D(@T*("`@("`@("`@
M("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(FUA<'!I;F=S(CH@6UTL
M#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@("`@("`@("`@("`B;6]D
M92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B<W1E<',B.B!;#0H@("`@
M("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B9W)E96XB
M+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(&YU;&P-"B`@("`@("`@("`@
M("`@?2P-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO
M<B(Z(")R960B+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(#$P,#`P#0H@
M("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@("`@("!]+`T*
M("`@("`@("`@(")U;FET(CH@(FYO;F4B#0H@("`@("`@('TL#0H@("`@("`@
M(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@
M>PT*("`@("`@("`B:"(Z(#@L#0H@("`@("`@(")W(CH@,3(L#0H@("`@("`@
M(")X(CH@,3(L#0H@("`@("`@(")Y(CH@,S(-"B`@("`@('TL#0H@("`@("`B
M:60B.B`R-BP-"B`@("`@(")I;G1E<G9A;"(Z("(Q;2(L#0H@("`@("`B;W!T
M:6]N<R(Z('L-"B`@("`@("`@(FQE9V5N9"(Z('L-"B`@("`@("`@("`B8V%L
M8W,B.B!;72P-"B`@("`@("`@("`B9&ES<&QA>4UO9&4B.B`B;&ES="(L#0H@
M("`@("`@("`@(G!L86-E;65N="(Z(")B;W1T;VTB+`T*("`@("`@("`@(")S
M:&]W3&5G96YD(CH@=')U90T*("`@("`@("!]+`T*("`@("`@("`B=&]O;'1I
M<"(Z('L-"B`@("`@("`@("`B;6]D92(Z(")S:6YG;&4B+`T*("`@("`@("`@
M(")S;W)T(CH@(FYO;F4B#0H@("`@("`@('T-"B`@("`@('TL#0H@("`@("`B
M=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@("`@(")D871A8F%S92(Z
M(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@
M("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD
M871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E
M?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*
M("`@("`@("`@("`@(F9R;VTB.B![#0H@("`@("`@("`@("`@(")P<F]P97)T
M>2(Z('L-"B`@("`@("`@("`@("`@("`B;F%M92(Z(")!9'AM;VY);F=E<W1O
M<E)E<75E<W1S4F5C96EV9614;W1A;"(L#0H@("`@("`@("`@("`@("`@(G1Y
M<&4B.B`B<W1R:6YG(@T*("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M("`B='EP92(Z(")P<F]P97)T>2(-"B`@("`@("`@("`@('TL#0H@("`@("`@
M("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS
M(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@
M("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@
M("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B
M86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-
M"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6PT*("`@("`@("`@("`@
M("`@('L-"B`@("`@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%L-"B`@
M("`@("`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@("`@("`@(")O
M<&5R871O<B(Z('L-"B`@("`@("`@("`@("`@("`@("`@("`@(")N86UE(CH@
M(CT](BP-"B`@("`@("`@("`@("`@("`@("`@("`@(")V86QU92(Z("(B#0H@
M("`@("`@("`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@("`@("`@
M("`B<')O<&5R='DB.B![#0H@("`@("`@("`@("`@("`@("`@("`@("`B;F%M
M92(Z(")5;F1E<FQA>4YA;64B+`T*("`@("`@("`@("`@("`@("`@("`@("`@
M(G1Y<&4B.B`B<W1R:6YG(@T*("`@("`@("`@("`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`@("`@("`@("`@(G1Y<&4B.B`B;W!E<F%T;W(B#0H@("`@
M("`@("`@("`@("`@("`@('T-"B`@("`@("`@("`@("`@("`@(%TL#0H@("`@
M("`@("`@("`@("`@("`B='EP92(Z(")O<B(-"B`@("`@("`@("`@("`@("!]
M#0H@("`@("`@("`@("`@(%TL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N
M9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P
M;'5G:6Y697)S:6]N(CH@(C0N-"XQ(BP-"B`@("`@("`@("`B<75E<GDB.B`B
M061X;6]N26YG97-T;W)1=65U95-I>F5<;GP@=VAE<F4@)%]?=&EM949I;'1E
M<BA4:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T97(@/3T@7"(D0VQU<W1E<EPB
M7&Y\('=H97)E($-O;G1A:6YE<B`]/2!<(FEN9V5S=&]R7")<;GP@97AT96YD
M('%U975E/71O<W1R:6YG*$QA8F5L<RYQ=65U92E<;GP@=VAE<F4@<75E=64@
M/3T@7")U<&QO861<(EQN?"!M86ME+7-E<FEE<R!686QU93US=6TH5F%L=64I
M(&1E9F%U;'0@/2`P(&]N(%1I;65S=&%M<"!F<F]M("1?7W1I;65&<F]M('1O
M("1?7W1I;654;R!S=&5P("1?7W1I;65);G1E<G9A;"!B>2!Q=65U92P@2&]S
M=%QN?"!E>'1E;F0@<W1A=',]<V5R:65S7W-T871S7V1Y;F%M:6,H5F%L=64I
M7&Y\(&]R9&5R(&)Y('1O<F5A;"AS=&%T<RYM87@I(&1E<V-<;GP@;&EM:70@
M,3!<;GP@<')O:F5C="!Q=65U92P@2&]S="P@5&EM97-T86UP+"!686QU95QN
M7&XB+`T*("`@("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@
M("`@(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z
M('1R=64L#0H@("`@("`@("`@(G)E9DED(CH@(D$B+`T*("`@("`@("`@(")R
M97-U;'1&;W)M870B.B`B=&EM95]S97)I97-?861X7W-E<FEE<R(-"B`@("`@
M("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z(")5<&QO860@475E=64@
M4VEZ92(L#0H@("`@("`B='EP92(Z(")T:6UE<V5R:65S(@T*("`@('TL#0H@
M("`@>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@
M(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@
M("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V5](@T*("`@("`@?2P-"B`@("`@
M(")F:65L9$-O;F9I9R(Z('L-"B`@("`@("`@(F1E9F%U;'1S(CH@>PT*("`@
M("`@("`@(")C;VQO<B(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@(G!A;&5T
M=&4M8VQA<W-I8R(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")C=7-T;VTB
M.B![#0H@("`@("`@("`@("`B87AI<T)O<F1E<E-H;W<B.B!F86QS92P-"B`@
M("`@("`@("`@(")A>&ES0V5N=&5R961:97)O(CH@9F%L<V4L#0H@("`@("`@
M("`@("`B87AI<T-O;&]R36]D92(Z(")T97AT(BP-"B`@("`@("`@("`@(")A
M>&ES3&%B96PB.B`B(BP-"B`@("`@("`@("`@(")A>&ES4&QA8V5M96YT(CH@
M(F%U=&\B+`T*("`@("`@("`@("`@(F%X:7-3;V9T36EN(CH@,"P-"B`@("`@
M("`@("`@(")B87)!;&EG;FUE;G0B.B`M,2P-"B`@("`@("`@("`@(")D<F%W
M4W1Y;&4B.B`B;&EN92(L#0H@("`@("`@("`@("`B9FEL;$]P86-I='DB.B`P
M+`T*("`@("`@("`@("`@(F=R861I96YT36]D92(Z(")N;VYE(BP-"B`@("`@
M("`@("`@(")H:61E1G)O;2(Z('L-"B`@("`@("`@("`@("`@(FQE9V5N9"(Z
M(&9A;'-E+`T*("`@("`@("`@("`@("`B=&]O;'1I<"(Z(&9A;'-E+`T*("`@
M("`@("`@("`@("`B=FEZ(CH@9F%L<V4-"B`@("`@("`@("`@('TL#0H@("`@
M("`@("`@("`B:6YS97)T3G5L;',B.B!F86QS92P-"B`@("`@("`@("`@(")L
M:6YE26YT97)P;VQA=&EO;B(Z(")L:6YE87(B+`T*("`@("`@("`@("`@(FQI
M;F57:61T:"(Z(#$L#0H@("`@("`@("`@("`B<&]I;G13:7IE(CH@,"P-"B`@
M("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z('L-"B`@("`@("`@("`@
M("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@
M("`@(")S:&]W4&]I;G1S(CH@(F%U=&\B+`T*("`@("`@("`@("`@(G-P86Y.
M=6QL<R(Z(&9A;'-E+`T*("`@("`@("`@("`@(G-T86-K:6YG(CH@>PT*("`@
M("`@("`@("`@("`B9W)O=7`B.B`B02(L#0H@("`@("`@("`@("`@(")M;V1E
M(CH@(FYO;F4B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G1H<F5S
M:&]L9'-3='EL92(Z('L-"B`@("`@("`@("`@("`@(FUO9&4B.B`B9&%S:&5D
M(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(FUA
M<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@("`@
M("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B<W1E
M<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L
M;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(&YU;&P-
M"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@
M("`@("`@(")C;VQO<B(Z(")L:6=H="UY96QL;W<B+`T*("`@("`@("`@("`@
M("`@(")V86QU92(Z(#4P,#`-"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@
M("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")S96UI+61A<FLM
M<F5D(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`Q,#`P,`T*("`@("`@
M("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@
M("`@("`B=6YI="(Z(")N;VYE(@T*("`@("`@("!]+`T*("`@("`@("`B;W9E
M<G)I9&5S(CH@6UT-"B`@("`@('TL#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@
M("`@("`@(F@B.B`X+`T*("`@("`@("`B=R(Z(#$R+`T*("`@("`@("`B>"(Z
M(#$R+`T*("`@("`@("`B>2(Z(#0P#0H@("`@("!]+`T*("`@("`@(FED(CH@
M,C<L#0H@("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B
M.B![#0H@("`@("`@(")L96=E;F0B.B![#0H@("`@("`@("`@(F-A;&-S(CH@
M6UTL#0H@("`@("`@("`@(F1I<W!L87E-;V1E(CH@(FQI<W0B+`T*("`@("`@
M("`@(")P;&%C96UE;G0B.B`B8F]T=&]M(BP-"B`@("`@("`@("`B<VAO=TQE
M9V5N9"(Z('1R=64-"B`@("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![
M#0H@("`@("`@("`@(FUO9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R
M="(Z(")N;VYE(@T*("`@("`@("!]#0H@("`@("!]+`T*("`@("`@(G1A<F=E
M=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T
M<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@
M("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O
M=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@
M("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@
M("`@("`@(")F<F]M(CH@>PT*("`@("`@("`@("`@("`B<')O<&5R='DB.B![
M#0H@("`@("`@("`@("`@("`@(FYA;64B.B`B061X;6]N26YG97-T;W)297%U
M97-T<U)E8V5I=F5D5&]T86PB+`T*("`@("`@("`@("`@("`@(")T>7!E(CH@
M(G-T<FEN9R(-"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@(G1Y
M<&4B.B`B<')O<&5R='DB#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=
M+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X
M<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-
M"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@
M("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%L-"B`@("`@("`@("`@("`@("![
M#0H@("`@("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;#0H@("`@("`@
M("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`@("`@("`B;W!E<F%T
M;W(B.B![#0H@("`@("`@("`@("`@("`@("`@("`@("`B;F%M92(Z("(]/2(L
M#0H@("`@("`@("`@("`@("`@("`@("`@("`B=F%L=64B.B`B(@T*("`@("`@
M("`@("`@("`@("`@("`@('TL#0H@("`@("`@("`@("`@("`@("`@("`@(G!R
M;W!E<G1Y(CH@>PT*("`@("`@("`@("`@("`@("`@("`@("`@(FYA;64B.B`B
M56YD97)L87E.86UE(BP-"B`@("`@("`@("`@("`@("`@("`@("`@(")T>7!E
M(CH@(G-T<FEN9R(-"B`@("`@("`@("`@("`@("`@("`@("!]+`T*("`@("`@
M("`@("`@("`@("`@("`@(")T>7!E(CH@(F]P97)A=&]R(@T*("`@("`@("`@
M("`@("`@("`@("!]#0H@("`@("`@("`@("`@("`@("!=+`T*("`@("`@("`@
M("`@("`@("`@(G1Y<&4B.B`B;W(B#0H@("`@("`@("`@("`@("`@?0T*("`@
M("`@("`@("`@("!=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN
M5F5R<VEO;B(Z("(T+C0N,2(L#0H@("`@("`@("`@(G%U97)Y(CH@(D%D>&UO
M;DEN9V5S=&]R475E=653:7IE7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM
M97-T86UP*5QN?"!W:&5R92!#;'5S=&5R(#T](%PB)$-L=7-T97)<(EQN?"!W
M:&5R92!#;VYT86EN97(@/3T@7")I;F=E<W1O<EPB7&Y\(&5X=&5N9"!Q=65U
M93UT;W-T<FEN9RA,86)E;',N<75E=64I7&Y\('=H97)E('%U975E(#T](%PB
M=')A;G-F97)<(EQN?"!M86ME+7-E<FEE<R!686QU93US=6TH5F%L=64I(&1E
M9F%U;'0@/2`P(&]N(%1I;65S=&%M<"!F<F]M("1?7W1I;65&<F]M('1O("1?
M7W1I;654;R!S=&5P("1?7W1I;65);G1E<G9A;"!B>2!Q=65U92P@2&]S=%QN
M?"!E>'1E;F0@<W1A=',]<V5R:65S7W-T871S7V1Y;F%M:6,H5F%L=64I7&Y\
M(&]R9&5R(&)Y('1O<F5A;"AS=&%T<RYM87@I(&1E<V-<;GP@;&EM:70@,3!<
M;GP@<')O:F5C="!Q=65U92P@2&]S="P@5&EM97-T86UP+"!686QU95QN7&XB
M+`T*("`@("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@
M(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R
M=64L#0H@("`@("`@("`@(G)E9DED(CH@(D$B+`T*("`@("`@("`@(")R97-U
M;'1&;W)M870B.B`B=&EM95]S97)I97-?861X7W-E<FEE<R(-"B`@("`@("`@
M?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z(")4<F%N<V9E<B!1=65U92!3
M:7IE(BP-"B`@("`@(")T>7!E(CH@(G1I;65S97)I97,B#0H@("`@?2P-"B`@
M("![#0H@("`@("`B8V]L;&%P<V5D(CH@9F%L<V4L#0H@("`@("`B9W)I9%!O
M<R(Z('L-"B`@("`@("`@(F@B.B`Q+`T*("`@("`@("`B=R(Z(#(T+`T*("`@
M("`@("`B>"(Z(#`L#0H@("`@("`@(")Y(CH@-#@-"B`@("`@('TL#0H@("`@
M("`B:60B.B`S-"P-"B`@("`@(")P86YE;',B.B!;72P-"B`@("`@(")T:71L
M92(Z(")297-O=7)C92!5<V%G92(L#0H@("`@("`B='EP92(Z(")R;W<B#0H@
M("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@
M(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R
M8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@("`@("!]
M+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B
M.B![#0H@("`@("`@("`@(F-U<W1O;2(Z('L-"B`@("`@("`@("`@(")H:61E
M1G)O;2(Z('L-"B`@("`@("`@("`@("`@(FQE9V5N9"(Z(&9A;'-E+`T*("`@
M("`@("`@("`@("`B=&]O;'1I<"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B
M=FEZ(CH@9F%L<V4-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<V-A
M;&5$:7-T<FEB=71I;VXB.B![#0H@("`@("`@("`@("`@(")T>7!E(CH@(FQI
M;F5A<B(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]#0H@("`@("`@('TL
M#0H@("`@("`@(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G
M<FED4&]S(CH@>PT*("`@("`@("`B:"(Z(#DL#0H@("`@("`@(")W(CH@,3(L
M#0H@("`@("`@(")X(CH@,"P-"B`@("`@("`@(GDB.B`T.0T*("`@("`@?2P-
M"B`@("`@(")I9"(Z(#,P+`T*("`@("`@(FEN=&5R=F%L(CH@(C%M(BP-"B`@
M("`@(")O<'1I;VYS(CH@>PT*("`@("`@("`B8V%L8W5L871E(CH@9F%L<V4L
M#0H@("`@("`@(")C86QC=6QA=&EO;B(Z('L-"B`@("`@("`@("`B>$)U8VME
M=',B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")C;W5N="(L#0H@("`@("`@
M("`@("`B=F%L=64B.B`B(@T*("`@("`@("`@('T-"B`@("`@("`@?2P-"B`@
M("`@("`@(F-E;&Q'87`B.B`Q+`T*("`@("`@("`B8V]L;W(B.B![#0H@("`@
M("`@("`@(F5X<&]N96YT(CH@,"XU+`T*("`@("`@("`@(")F:6QL(CH@(F1A
M<FLM;W)A;F=E(BP-"B`@("`@("`@("`B;6]D92(Z(")S8VAE;64B+`T*("`@
M("`@("`@(")R979E<G-E(CH@9F%L<V4L#0H@("`@("`@("`@(G-C86QE(CH@
M(F5X<&]N96YT:6%L(BP-"B`@("`@("`@("`B<V-H96UE(CH@(D]R86YG97,B
M+`T*("`@("`@("`@(")S=&5P<R(Z(#8T#0H@("`@("`@('TL#0H@("`@("`@
M(")E>&5M<&QA<G,B.B![#0H@("`@("`@("`@(F-O;&]R(CH@(G)G8F$H,C4U
M+#`L,C4U+#`N-RDB#0H@("`@("`@('TL#0H@("`@("`@(")F:6QT97)686QU
M97,B.B![#0H@("`@("`@("`@(FQE(CH@,64M.0T*("`@("`@("!]+`T*("`@
M("`@("`B;&5G96YD(CH@>PT*("`@("`@("`@(")S:&]W(CH@=')U90T*("`@
M("`@("!]+`T*("`@("`@("`B<F]W<T9R86UE(CH@>PT*("`@("`@("`@(")L
M87EO=70B.B`B875T;R(-"B`@("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B
M.B![#0H@("`@("`@("`@(FUO9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B
M<VAO=T-O;&]R4V-A;&4B.B!F86QS92P-"B`@("`@("`@("`B>4AI<W1O9W)A
M;2(Z(&9A;'-E#0H@("`@("`@('TL#0H@("`@("`@(")Y07AI<R(Z('L-"B`@
M("`@("`@("`B87AI<U!L86-E;65N="(Z(")L969T(BP-"B`@("`@("`@("`B
M<F5V97)S92(Z(&9A;'-E#0H@("`@("`@('T-"B`@("`@('TL#0H@("`@("`B
M<&QU9VEN5F5R<VEO;B(Z("(Q,"XT+C$Q(BP-"B`@("`@(")T87)G971S(CH@
M6PT*("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@
M("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T
M87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U
M:60B.B`B)'M$871A<V]U<F-E?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@
M(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@
M("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@
M("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN
M5F5R<VEO;B(Z("(U+C`N-2(L#0H@("`@("`@("`@(G%U97)Y(CH@(FQE="!B
M=6-K971S/34P.UQN;&5T(&)I;E-I>F4]=&]S8V%L87(H0V]N=&%I;F5R0W!U
M57-A9V5396-O;F1S5&]T86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE
M<W1A;7`I7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W:&5R
M92!,86)E;',N8W!U/3U<(G1O=&%L7")<;GP@=VAE<F4@3&%B96QS+FYA;65S
M<&%C92`]/2!<(F%D>"UM;VY<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R
M:6YG*$QA8F5L<RYN86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H
M3&%B96QS+G!O9"E<;GP@97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E
M;',N8V]N=&%I;F5R*5QN?"!W:&5R92!#;VYT86EN97(@/3T@7")I;F=E<W1O
M<EPB7&Y\(&EN=F]K92!P<F]M7V1E;'1A*"E<;GP@<W5M;6%R:7IE(%9A;'5E
M/2AM87@H5F%L=64I+S8P*2!B>2!B:6XH5&EM97-T86UP+"`V,#`P,&US*2P@
M3F%M97-P86-E+"!0;V1<;GP@<W5M;6%R:7IE(&UI;BA686QU92DL(&UA>"A6
M86QU92DL(&%V9RA686QU92E<;GP@97AT96YD($1I9F8]*&UA>%]686QU92`M
M(&UI;E]686QU92D@+R!B=6-K971S7&Y\('!R;VIE8W0@1&EF9BD[7&Y#;VYT
M86EN97)#<'55<V%G95-E8V]N9'-4;W1A;%QN?"!W:&5R92`D7U]T:6UE1FEL
M=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB
M7&Y\('=H97)E($QA8F5L<RYC<'4]/5PB=&]T86Q<(EQN?"!W:&5R92!,86)E
M;',N;F%M97-P86-E(#T](%PB861X+6UO;EPB7&Y\(&5X=&5N9"!.86UE<W!A
M8V4]=&]S=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@97AT96YD(%!O9#UT
M;W-T<FEN9RA,86)E;',N<&]D*5QN?"!E>'1E;F0@0V]N=&%I;F5R/71O<W1R
M:6YG*$QA8F5L<RYC;VYT86EN97(I7&Y\('=H97)E($-O;G1A:6YE<B`]/2!<
M(FEN9V5S=&]R7")<;GP@:6YV;VME('!R;VU?<F%T92@I7&Y\('-U;6UA<FEZ
M92!686QU93UR;W5N9"AA=F<H5F%L=64I*S`N,#`P-2PS*2!B>2!B:6XH5&EM
M97-T86UP+"`V,#`P,&US*2P@4&]D7&Y\('-U;6UA<FEZ92!686QU93UC;W5N
M="@I(&)Y(%1I;65S=&%M<"P@0FEN/7)O=6YD*&)I;BA686QU92P@=&]R96%L
M*&)I;E-I>F4I*2P@,RE<;GP@;W)D97(@8GD@5&EM97-T86UP(&%S8UQN?"!P
M<F]J96-T(%1I;65S=&%M<"P@=&]S=')I;F<H0FEN*2P@5F%L=65<;B(L#0H@
M("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U
M97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-
M"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@("`@("`@("`@(G)E<W5L=$9O
M<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@("`@?0T*("`@("`@72P-"B`@
M("`@(")T:71L92(Z("));F=E<W1O<B!#4%4@57-A9V4@*$-O<F5S*2(L#0H@
M("`@("`B='EP92(Z(")H96%T;6%P(@T*("`@('TL#0H@("`@>PT*("`@("`@
M(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V5](@T*("`@("`@?2P-"B`@("`@(")F:65L9$-O;F9I
M9R(Z('L-"B`@("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@(")C=7-T
M;VTB.B![#0H@("`@("`@("`@("`B:&ED949R;VTB.B![#0H@("`@("`@("`@
M("`@(")L96=E;F0B.B!F86QS92P-"B`@("`@("`@("`@("`@(G1O;VQT:7`B
M.B!F86QS92P-"B`@("`@("`@("`@("`@(G9I>B(Z(&9A;'-E#0H@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@(G-C86QE1&ES=')I8G5T:6]N(CH@>PT*
M("`@("`@("`@("`@("`B='EP92(Z(")L:6YE87(B#0H@("`@("`@("`@("!]
M#0H@("`@("`@("`@?0T*("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S
M(CH@6UT-"B`@("`@('TL#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@
M(F@B.B`Y+`T*("`@("`@("`B=R(Z(#$R+`T*("`@("`@("`B>"(Z(#$R+`T*
M("`@("`@("`B>2(Z(#0Y#0H@("`@("!]+`T*("`@("`@(FED(CH@,S$L#0H@
M("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(FUA>%!E<E)O=R(Z(#(L
M#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(F-A;&-U;&%T92(Z(&9A
M;'-E+`T*("`@("`@("`B8V5L;$=A<"(Z(#$L#0H@("`@("`@(")C;VQO<B(Z
M('L-"B`@("`@("`@("`B97AP;VYE;G0B.B`P+C4L#0H@("`@("`@("`@(F9I
M;&PB.B`B9&%R:RUO<F%N9V4B+`T*("`@("`@("`@(")M;V1E(CH@(G-C:&5M
M92(L#0H@("`@("`@("`@(G)E=F5R<V4B.B!F86QS92P-"B`@("`@("`@("`B
M<V-A;&4B.B`B97AP;VYE;G1I86PB+`T*("`@("`@("`@(")S8VAE;64B.B`B
M4F19;$=N(BP-"B`@("`@("`@("`B<W1E<',B.B`V-`T*("`@("`@("!]+`T*
M("`@("`@("`B97AE;7!L87)S(CH@>PT*("`@("`@("`@(")C;VQO<B(Z(")R
M9V)A*#(U-2PP+#(U-2PP+C<I(@T*("`@("`@("!]+`T*("`@("`@("`B9FEL
M=&5R5F%L=65S(CH@>PT*("`@("`@("`@(")L92(Z(#%E+3D-"B`@("`@("`@
M?2P-"B`@("`@("`@(FQE9V5N9"(Z('L-"B`@("`@("`@("`B<VAO=R(Z('1R
M=64-"B`@("`@("`@?2P-"B`@("`@("`@(G)O=W-&<F%M92(Z('L-"B`@("`@
M("`@("`B;&%Y;W5T(CH@(F%U=&\B#0H@("`@("`@('TL#0H@("`@("`@(")T
M;V]L=&EP(CH@>PT*("`@("`@("`@(")M;V1E(CH@(G-I;F=L92(L#0H@("`@
M("`@("`@(G-H;W=#;VQO<E-C86QE(CH@9F%L<V4L#0H@("`@("`@("`@(GE(
M:7-T;V=R86TB.B!F86QS90T*("`@("`@("!]+`T*("`@("`@("`B>4%X:7,B
M.B![#0H@("`@("`@("`@(F%X:7-0;&%C96UE;G0B.B`B;&5F="(L#0H@("`@
M("`@("`@(G)E=F5R<V4B.B!F86QS92P-"B`@("`@("`@("`B=6YI="(Z(")D
M96-B>71E<R(-"B`@("`@("`@?0T*("`@("`@?2P-"B`@("`@(")P;'5G:6Y6
M97)S:6]N(CH@(C$P+C0N,3$B+`T*("`@("`@(G)E<&5A="(Z(").86UE<W!A
M8V4B+`T*("`@("`@(G)E<&5A=$1I<F5C=&EO;B(Z(")V(BP-"B`@("`@(")T
M87)G971S(CH@6PT*("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A
M;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@
M("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R
M869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@
M("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E?2(-"B`@("`@("`@("!]+`T*
M("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P
M0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@
M("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@
M("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I
M;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@
M("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z
M(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@
M("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y
M(CH@(D-O;G1A:6YE<DUE;6]R>5=O<FMI;F=3971">71E<UQN?"!W:&5R92`D
M7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D
M0VQU<W1E<EPB7&Y\(&5X=&5N9"!.86UE<W!A8V4]=&]S=')I;F<H3&%B96QS
M+FYA;65S<&%C92E<;GP@97AT96YD(%!O9#UT;W-T<FEN9RA,86)E;',N<&]D
M*5QN?"!E>'1E;F0@0V]N=&%I;F5R/71O<W1R:6YG*$QA8F5L<RYC;VYT86EN
M97(I7&Y\('=H97)E($YA;65S<&%C92`]/2!<(F%D>"UM;VY<(EQN?"!W:&5R
M92!#;VYT86EN97(@/3T@7")I;F=E<W1O<EPB7&Y\(&5X=&5N9"!686QU93U6
M86QU95QN?"!S=6UM87)I>F4@5F%L=64]879G*%9A;'5E*2!B>2!B:6XH5&EM
M97-T86UP+"`V,#`P,&US*2P@3F%M97-P86-E+"!0;V1<;GP@<W5M;6%R:7IE
M(%9A;'5E/6-O=6YT*"D@8GD@5&EM97-T86UP+"!":6X]8FEN*%9A;'5E+"`U
M938I+"!.86UE<W!A8V5<;GP@<')O:F5C="!4:6UE<W1A;7`L('1O<W1R:6YG
M*$)I;BDL(%9A;'5E7&Y\(&]R9&5R(&)Y(%1I;65S=&%M<"!A<V-<;B(L#0H@
M("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U
M97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-
M"B`@("`@("`@("`B<F5F260B.B`B5V]R:VEN9U-E="(L#0H@("`@("`@("`@
M(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@("`@?0T*("`@
M("`@72P-"B`@("`@(")T:71L92(Z("));F=E<W1O<B!-96T@57-A9V4B+`T*
M("`@("`@(G1Y<&4B.B`B:&5A=&UA<"(-"B`@("!]+`T*("`@('L-"B`@("`@
M(")D871A<V]U<F-E(CH@>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z
M=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U:60B
M.B`B)'M$871A<V]U<F-E?2(-"B`@("`@('TL#0H@("`@("`B9FEE;&1#;VYF
M:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z('L-"B`@("`@("`@("`B8W5S
M=&]M(CH@>PT*("`@("`@("`@("`@(FAI9&5&<F]M(CH@>PT*("`@("`@("`@
M("`@("`B;&5G96YD(CH@9F%L<V4L#0H@("`@("`@("`@("`@(")T;V]L=&EP
M(CH@9F%L<V4L#0H@("`@("`@("`@("`@(")V:7HB.B!F86QS90T*("`@("`@
M("`@("`@?2P-"B`@("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z('L-
M"B`@("`@("`@("`@("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@("`@
M?0T*("`@("`@("`@('T-"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R:61E
M<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@("`@
M(")H(CH@.2P-"B`@("`@("`@(G<B.B`Q,BP-"B`@("`@("`@(G@B.B`P+`T*
M("`@("`@("`B>2(Z(#4X#0H@("`@("!]+`T*("`@("`@(FED(CH@,S4L#0H@
M("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B.B![#0H@
M("`@("`@(")C86QC=6QA=&4B.B!F86QS92P-"B`@("`@("`@(F-A;&-U;&%T
M:6]N(CH@>PT*("`@("`@("`@(")X0G5C:V5T<R(Z('L-"B`@("`@("`@("`@
M(")M;V1E(CH@(F-O=6YT(BP-"B`@("`@("`@("`@(")V86QU92(Z("(B#0H@
M("`@("`@("`@?0T*("`@("`@("!]+`T*("`@("`@("`B8V5L;$=A<"(Z(#$L
M#0H@("`@("`@(")C;VQO<B(Z('L-"B`@("`@("`@("`B97AP;VYE;G0B.B`P
M+C4L#0H@("`@("`@("`@(F9I;&PB.B`B9&%R:RUO<F%N9V4B+`T*("`@("`@
M("`@(")M;V1E(CH@(G-C:&5M92(L#0H@("`@("`@("`@(G)E=F5R<V4B.B!F
M86QS92P-"B`@("`@("`@("`B<V-A;&4B.B`B97AP;VYE;G1I86PB+`T*("`@
M("`@("`@(")S8VAE;64B.B`B3W)A;F=E<R(L#0H@("`@("`@("`@(G-T97!S
M(CH@,3(X#0H@("`@("`@('TL#0H@("`@("`@(")E>&5M<&QA<G,B.B![#0H@
M("`@("`@("`@(F-O;&]R(CH@(G)G8F$H,C4U+#`L,C4U+#`N-RDB#0H@("`@
M("`@('TL#0H@("`@("`@(")F:6QT97)686QU97,B.B![#0H@("`@("`@("`@
M(FQE(CH@,64M.0T*("`@("`@("!]+`T*("`@("`@("`B;&5G96YD(CH@>PT*
M("`@("`@("`@(")S:&]W(CH@=')U90T*("`@("`@("!]+`T*("`@("`@("`B
M<F]W<T9R86UE(CH@>PT*("`@("`@("`@(")L87EO=70B.B`B875T;R(-"B`@
M("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@("`@(FUO
M9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<VAO=T-O;&]R4V-A;&4B.B!F
M86QS92P-"B`@("`@("`@("`B>4AI<W1O9W)A;2(Z(&9A;'-E#0H@("`@("`@
M('TL#0H@("`@("`@(")Y07AI<R(Z('L-"B`@("`@("`@("`B87AI<U!L86-E
M;65N="(Z(")L969T(BP-"B`@("`@("`@("`B<F5V97)S92(Z(&9A;'-E#0H@
M("`@("`@('T-"B`@("`@('TL#0H@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(Q
M,"XT+C$Q(BP-"B`@("`@(")T87)G971S(CH@6PT*("`@("`@("![#0H@("`@
M("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z
M(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@
M("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD
M871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E
M?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*
M("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@
M("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T
M>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE
M<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@
M("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@
M("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L
M#0H@("`@("`@("`@(G%U97)Y(CH@(FQE="!B=6-K971S/34P.UQN;&5T(&)I
M;E-I>F4]=&]S8V%L87(H0V]N=&%I;F5R0W!U57-A9V5396-O;F1S5&]T86Q<
M;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H97)E($-L
M=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W:&5R92!,86)E;',N8W!U/3U<(G1O
M=&%L7")<;GP@=VAE<F4@3&%B96QS+FYA;65S<&%C92`]/2!<(F%D>"UM;VY<
M(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG*$QA8F5L<RYN86UE<W!A
M8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B96QS+G!O9"E<;GP@97AT
M96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E;',N8V]N=&%I;F5R*5QN?"!W
M:&5R92!#;VYT86EN97(@/3T@7")C;VQL96-T;W)<(EQN?"!I;G9O:V4@<')O
M;5]D96QT82@I7&Y\('-U;6UA<FEZ92!686QU93TH;6%X*%9A;'5E*2\V,"D@
M8GD@8FEN*%1I;65S=&%M<"P@-C`P,#!M<RDL($YA;65S<&%C92P@4&]D7&Y\
M('-U;6UA<FEZ92!M:6XH5F%L=64I+"!M87@H5F%L=64I+"!A=F<H5F%L=64I
M7&Y\(&5X=&5N9"!$:69F/2AM87A?5F%L=64@+2!M:6Y?5F%L=64I("\@8G5C
M:V5T<UQN?"!P<F]J96-T($1I9F8I.UQN0V]N=&%I;F5R0W!U57-A9V5396-O
M;F1S5&]T86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\
M('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W:&5R92!,86)E;',N
M8W!U/3U<(G1O=&%L7")<;GP@=VAE<F4@3&%B96QS+FYA;65S<&%C92`]/2!<
M(F%D>"UM;VY<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG*$QA8F5L
M<RYN86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B96QS+G!O
M9"E<;GP@97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E;',N8V]N=&%I
M;F5R*5QN?"!W:&5R92!#;VYT86EN97(@/3T@7")C;VQL96-T;W)<(EQN?"!I
M;G9O:V4@<')O;5]R871E*"E<;GP@<W5M;6%R:7IE(%9A;'5E/7)O=6YD*&%V
M9RA686QU92DK,"XP,#`U+#,I(&)Y(&)I;BA4:6UE<W1A;7`L(#8P,#`P;7,I
M+"!0;V1<;GP@<W5M;6%R:7IE(%9A;'5E/6-O=6YT*"D@8GD@5&EM97-T86UP
M+"!":6X]<F]U;F0H8FEN*%9A;'5E+"!T;W)E86PH8FEN4VEZ92DI+"`S*5QN
M?"!O<F1E<B!B>2!4:6UE<W1A;7`@87-C7&Y\('!R;VIE8W0@5&EM97-T86UP
M+"!T;W-T<FEN9RA":6XI+"!686QU95QN(BP-"B`@("`@("`@("`B<75E<GE3
M;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L
M#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)
M9"(Z(")5<V%G92(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE
M7W-E<FEE<R(-"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z
M(")#;VQL96-T;W(@0U!5(%5S86=E("A#;W)E<RDB+`T*("`@("`@(G1Y<&4B
M.B`B:&5A=&UA<"(-"B`@("!]+`T*("`@('L-"B`@("`@(")D871A<V]U<F-E
M(CH@>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP
M;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U
M<F-E?2(-"B`@("`@('TL#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@
M("`@(")D969A=6QT<R(Z('L-"B`@("`@("`@("`B8W5S=&]M(CH@>PT*("`@
M("`@("`@("`@(FAI9&5&<F]M(CH@>PT*("`@("`@("`@("`@("`B;&5G96YD
M(CH@9F%L<V4L#0H@("`@("`@("`@("`@(")T;V]L=&EP(CH@9F%L<V4L#0H@
M("`@("`@("`@("`@(")V:7HB.B!F86QS90T*("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z('L-"B`@("`@("`@("`@
M("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@("`@?0T*("`@("`@("`@
M('TL#0H@("`@("`@("`@(F9I96QD36EN36%X(CH@9F%L<V4-"B`@("`@("`@
M?2P-"B`@("`@("`@(F]V97)R:61E<R(Z(%M=#0H@("`@("!]+`T*("`@("`@
M(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@.2P-"B`@("`@("`@(G<B.B`Q
M,BP-"B`@("`@("`@(G@B.B`Q,BP-"B`@("`@("`@(GDB.B`U.`T*("`@("`@
M?2P-"B`@("`@(")I9"(Z(#,S+`T*("`@("`@(FEN=&5R=F%L(CH@(C%M(BP-
M"B`@("`@(")M87A097)2;W<B.B`R+`T*("`@("`@(F]P=&EO;G,B.B![#0H@
M("`@("`@(")C86QC=6QA=&4B.B!F86QS92P-"B`@("`@("`@(F-E;&Q'87`B
M.B`Q+`T*("`@("`@("`B8V]L;W(B.B![#0H@("`@("`@("`@(F5X<&]N96YT
M(CH@,2P-"B`@("`@("`@("`B9FEL;"(Z(")L:6=H="UY96QL;W<B+`T*("`@
M("`@("`@(")M;V1E(CH@(G-C:&5M92(L#0H@("`@("`@("`@(G)E=F5R<V4B
M.B!F86QS92P-"B`@("`@("`@("`B<V-A;&4B.B`B97AP;VYE;G1I86PB+`T*
M("`@("`@("`@(")S8VAE;64B.B`B4F19;$=N(BP-"B`@("`@("`@("`B<W1E
M<',B.B`Q,C@-"B`@("`@("`@?2P-"B`@("`@("`@(F5X96UP;&%R<R(Z('L-
M"B`@("`@("`@("`B8V]L;W(B.B`B<F=B82@R-34L,"PR-34L,"XW*2(-"B`@
M("`@("`@?2P-"B`@("`@("`@(F9I;'1E<E9A;'5E<R(Z('L-"B`@("`@("`@
M("`B;&4B.B`Q92TY#0H@("`@("`@('TL#0H@("`@("`@(")L96=E;F0B.B![
M#0H@("`@("`@("`@(G-H;W<B.B!T<G5E#0H@("`@("`@('TL#0H@("`@("`@
M(")R;W=S1G)A;64B.B![#0H@("`@("`@("`@(FQA>6]U="(Z(")A=71O(@T*
M("`@("`@("!]+`T*("`@("`@("`B=&]O;'1I<"(Z('L-"B`@("`@("`@("`B
M;6]D92(Z(")S:6YG;&4B+`T*("`@("`@("`@(")S:&]W0V]L;W)38V%L92(Z
M(&9A;'-E+`T*("`@("`@("`@(")Y2&ES=&]G<F%M(CH@=')U90T*("`@("`@
M("!]+`T*("`@("`@("`B>4%X:7,B.B![#0H@("`@("`@("`@(F%X:7-0;&%C
M96UE;G0B.B`B;&5F="(L#0H@("`@("`@("`@(G)E=F5R<V4B.B!F86QS92P-
M"B`@("`@("`@("`B=6YI="(Z(")D96-B>71E<R(-"B`@("`@("`@?0T*("`@
M("`@?2P-"B`@("`@(")P;'5G:6Y697)S:6]N(CH@(C$P+C0N,3$B+`T*("`@
M("`@(G)E<&5A=$1I<F5C=&EO;B(Z(")V(BP-"B`@("`@(")T87)G971S(CH@
M6PT*("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@
M("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T
M87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U
M:60B.B`B)'M$871A<V]U<F-E?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@
M(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@
M("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@
M("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN
M5F5R<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A
M:6YE<DUE;6]R>5=O<FMI;F=3971">71E<UQN?"!W:&5R92`D7U]T:6UE1FEL
M=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@3&%B96QS("%H87,@7")I9%PB7&Y\
M('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!E>'1E;F0@3F%M97-P
M86-E/71O<W1R:6YG*$QA8F5L<RYN86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]
M=&]S=')I;F<H3&%B96QS+G!O9"E<;GP@97AT96YD($-O;G1A:6YE<CUT;W-T
M<FEN9RA,86)E;',N8V]N=&%I;F5R*5QN?"!W:&5R92!.86UE<W!A8V4@/3T@
M7")A9'@M;6]N7")<;GP@=VAE<F4@0V]N=&%I;F5R(#T](%PB8V]L;&5C=&]R
M7")<;GP@97AT96YD(%9A;'5E/59A;'5E7&Y\('-U;6UA<FEZ92!686QU93UA
M=F<H5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L(#8P,#`P;7,I+"!.86UE<W!A
M8V4L(%!O9%QN?"!S=6UM87)I>F4@5F%L=64]8V]U;G0H*2!B>2!4:6UE<W1A
M;7`L($)I;CUB:6XH5F%L=64L(#%E-RDL($YA;65S<&%C95QN?"!P<F]J96-T
M(%1I;65S=&%M<"P@=&]S=')I;F<H0FEN*2P@5F%L=65<;GP@;W)D97(@8GD@
M5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B
M<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@
M("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")7;W)K
M:6YG4V5T(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?<V5R
M:65S(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1I=&QE(CH@(D-O
M;&QE8W1O<B!-96T@57-A9V4B+`T*("`@("`@(G1Y<&4B.B`B:&5A=&UA<"(-
M"B`@("!]#0H@(%TL#0H@(")R969R97-H(CH@(B(L#0H@(")R979I<VEO;B(Z
M(#$L#0H@(")S8VAE;6%697)S:6]N(CH@,SDL#0H@(")T86=S(CH@6UTL#0H@
M(")T96UP;&%T:6YG(CH@>PT*("`@(")L:7-T(CH@6PT*("`@("`@>PT*("`@
M("`@("`B:&ED92(Z(#`L#0H@("`@("`@(")I;F-L=61E06QL(CH@9F%L<V4L
M#0H@("`@("`@(")L86)E;"(Z(")$871A<V]U<F-E(BP-"B`@("`@("`@(FUU
M;'1I(CH@9F%L<V4L#0H@("`@("`@(")N86UE(CH@(D1A=&%S;W5R8V4B+`T*
M("`@("`@("`B;W!T:6]N<R(Z(%M=+`T*("`@("`@("`B<75E<GDB.B`B9W)A
M9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@
M("`B<75E<GE686QU92(Z("(B+`T*("`@("`@("`B<F5F<F5S:"(Z(#$L#0H@
M("`@("`@(")R96=E>"(Z("(B+`T*("`@("`@("`B<VMI<%5R;%-Y;F,B.B!F
M86QS92P-"B`@("`@("`@(G1Y<&4B.B`B9&%T87-O=7)C92(-"B`@("`@('TL
M#0H@("`@("![#0H@("`@("`@(")C=7)R96YT(CH@>PT*("`@("`@("`@(")S
M96QE8W1E9"(Z(&9A;'-E+`T*("`@("`@("`@(")T97AT(CH@(DUE=')I8W,B
M+`T*("`@("`@("`@(")V86QU92(Z(")-971R:6-S(@T*("`@("`@("!]+`T*
M("`@("`@("`B:&ED92(Z(#`L#0H@("`@("`@(")I;F-L=61E06QL(CH@9F%L
M<V4L#0H@("`@("`@(")L86)E;"(Z(")$871A8F%S92(L#0H@("`@("`@(")M
M=6QT:2(Z(&9A;'-E+`T*("`@("`@("`B;F%M92(Z(")$871A8F%S92(L#0H@
M("`@("`@(")O<'1I;VYS(CH@6PT*("`@("`@("`@('L-"B`@("`@("`@("`@
M(")S96QE8W1E9"(Z('1R=64L#0H@("`@("`@("`@("`B=&5X="(Z(")-971R
M:6-S(BP-"B`@("`@("`@("`@(")V86QU92(Z(")-971R:6-S(@T*("`@("`@
M("`@('T-"B`@("`@("`@72P-"B`@("`@("`@(G%U97)Y(CH@(DUE=')I8W,B
M+`T*("`@("`@("`B<75E<GE686QU92(Z("(B+`T*("`@("`@("`B<VMI<%5R
M;%-Y;F,B.B!F86QS92P-"B`@("`@("`@(G1Y<&4B.B`B8W5S=&]M(@T*("`@
M("`@?2P-"B`@("`@('L-"B`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@
M("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A
M=&%S;W5R8V4B+`T*("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E?2(-
M"B`@("`@("`@?2P-"B`@("`@("`@(F1E9FEN:71I;VXB.B`B0V]N=&%I;F5R
M0W!U57-A9V5396-O;F1S5&]T86P@?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I
M;65S=&%M<"D@?"!D:7-T:6YC="!#;'5S=&5R(BP-"B`@("`@("`@(FAI9&4B
M.B`P+`T*("`@("`@("`B:6YC;'5D94%L;"(Z(&9A;'-E+`T*("`@("`@("`B
M;75L=&DB.B!F86QS92P-"B`@("`@("`@(FYA;64B.B`B0VQU<W1E<B(L#0H@
M("`@("`@(")O<'1I;VYS(CH@6UTL#0H@("`@("`@(")Q=65R>2(Z('L-"B`@
M("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F-L=7-T97)5
M<FDB.B`B(BP-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@
M("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!"
M>2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@
M("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@
M("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO
M;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@
M("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@
M("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@
M(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@
M(")P;'5G:6Y697)S:6]N(CH@(C4N,"XU(BP-"B`@("`@("`@("`B<75E<GDB
M.B`B0V]N=&%I;F5R0W!U57-A9V5396-O;F1S5&]T86P@?"!W:&5R92`D7U]T
M:6UE1FEL=&5R*%1I;65S=&%M<"D@?"!D:7-T:6YC="!#;'5S=&5R(BP-"B`@
M("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E
M<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*
M("`@("`@("`@(")R97-U;'1&;W)M870B.B`B=&%B;&4B#0H@("`@("`@('TL
M#0H@("`@("`@(")R969R97-H(CH@,2P-"B`@("`@("`@(G)E9V5X(CH@(B(L
M#0H@("`@("`@(")S:VEP57)L4WEN8R(Z(&9A;'-E+`T*("`@("`@("`B<V]R
M="(Z(#`L#0H@("`@("`@(")T>7!E(CH@(G%U97)Y(@T*("`@("`@?0T*("`@
M(%T-"B`@?2P-"B`@(G1I;64B.B![#0H@("`@(F9R;VTB.B`B;F]W+39H(BP-
M"B`@("`B=&\B.B`B;F]W(@T*("!]+`T*("`B=&EM97!I8VME<B(Z('M]+`T*
M("`B=&EM97IO;F4B.B`B(BP-"B`@(G1I=&QE(CH@(DUE=')I8W,@4W1A=',B
7+`T*("`B=V5E:U-T87)T(CH@(B(-"GUI
`
end
SHAR_EOF
  (set 20 24 12 06 20 59 58 'dashboards/metrics-stats.json'
   eval "${shar_touch}") && \
  chmod 0644 'dashboards/metrics-stats.json'
if test $? -ne 0
then ${echo} "restore of dashboards/metrics-stats.json failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'dashboards/metrics-stats.json': 'MD5 check failed'
       ) << \SHAR_EOF
7248ab54086ae6570492522a78467202  dashboards/metrics-stats.json
SHAR_EOF

else
test `LC_ALL=C wc -c < 'dashboards/metrics-stats.json'` -ne 82148 && \
  ${echo} "restoration warning:  size of 'dashboards/metrics-stats.json' is not 82148"
  fi
# ============= dashboards/cluster-info.json ==============
if test ! -d 'dashboards'; then
  mkdir 'dashboards' || exit 1
fi
  sed 's/^X//' << 'SHAR_EOF' | uudecode &&
begin 600 dashboards/cluster-info.json
M>PT*("`B86YN;W1A=&EO;G,B.B![#0H@("`@(FQI<W0B.B!;#0H@("`@("![
M#0H@("`@("`@(")B=6EL=$EN(CH@,2P-"B`@("`@("`@(F1A=&%S;W5R8V4B
M.B![#0H@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X
M<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@(")U:60B.B`B)'M$871A
M<V]U<F-E.G1E>'1](@T*("`@("`@("!]+`T*("`@("`@("`B96YA8FQE(CH@
M=')U92P-"B`@("`@("`@(FAI9&4B.B!T<G5E+`T*("`@("`@("`B:6-O;D-O
M;&]R(CH@(G)G8F$H,"P@,C$Q+"`R-34L(#$I(BP-"B`@("`@("`@(FYA;64B
M.B`B06YN;W1A=&EO;G,@)B!!;&5R=',B+`T*("`@("`@("`B='EP92(Z(")D
M87-H8F]A<F0B#0H@("`@("!]#0H@("`@70T*("!]+`T*("`B961I=&%B;&4B
M.B!T<G5E+`T*("`B9FES8V%L665A<E-T87)T36]N=&@B.B`P+`T*("`B9W)A
M<&A4;V]L=&EP(CH@,"P-"B`@(FED(CH@,S,L#0H@(")L:6YK<R(Z(%M=+`T*
M("`B<&%N96QS(CH@6PT*("`@('L-"B`@("`@(")D871A<V]U<F-E(CH@>PT*
M("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M
M9&%T87-O=7)C92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E
M>'1](@T*("`@("`@?2P-"B`@("`@(")F:65L9$-O;F9I9R(Z('L-"B`@("`@
M("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@(")C;VQO<B(Z('L-"B`@("`@
M("`@("`@(")M;V1E(CH@(F-O;G1I;G5O=7,M1W)9;%)D(@T*("`@("`@("`@
M('TL#0H@("`@("`@("`@(F9I96QD36EN36%X(CH@9F%L<V4L#0H@("`@("`@
M("`@(FUA<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(FUA>"(Z(#$L#0H@("`@
M("`@("`@(FUI;B(Z(#`L#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@
M("`@("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B
M<W1E<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B
M8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(&YU
M;&P-"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@>PT*("`@("`@
M("`@("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@("`@("`@(")V
M86QU92(Z(#@P#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@
M("`@("`@("!]+`T*("`@("`@("`@(")U;FET(CH@(G!E<F-E;G1U;FET(@T*
M("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6UT-"B`@("`@('TL
M#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`X+`T*("`@("`@
M("`B=R(Z(#8L#0H@("`@("`@(")X(CH@,"P-"B`@("`@("`@(GDB.B`P#0H@
M("`@("!]+`T*("`@("`@(FED(CH@-"P-"B`@("`@(")O<'1I;VYS(CH@>PT*
M("`@("`@("`B9&ES<&QA>4UO9&4B.B`B;&-D(BP-"B`@("`@("`@(FUA>%9I
M>DAE:6=H="(Z(#,P,"P-"B`@("`@("`@(FUI;E9I>DAE:6=H="(Z(#$V+`T*
M("`@("`@("`B;6EN5FEZ5VED=&@B.B`X+`T*("`@("`@("`B;F%M95!L86-E
M;65N="(Z(")T;W`B+`T*("`@("`@("`B;W)I96YT871I;VXB.B`B:&]R:7IO
M;G1A;"(L#0H@("`@("`@(")R961U8V5/<'1I;VYS(CH@>PT*("`@("`@("`@
M(")C86QC<R(Z(%L-"B`@("`@("`@("`@(")L87-T3F]T3G5L;"(-"B`@("`@
M("`@("!=+`T*("`@("`@("`@(")F:65L9',B.B`B(BP-"B`@("`@("`@("`B
M=F%L=65S(CH@9F%L<V4-"B`@("`@("`@?2P-"B`@("`@("`@(G-H;W=5;F9I
M;&QE9"(Z('1R=64L#0H@("`@("`@(")S:7II;F<B.B`B875T;R(L#0H@("`@
M("`@(")T97AT(CH@>WTL#0H@("`@("`@(")V86QU94UO9&4B.B`B8V]L;W(B
M#0H@("`@("!]+`T*("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B,3`N-"XQ,2(L
M#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@("`@(")/
M<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC
M<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B
M='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C
M92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-
M"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@
M("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R97-S
M:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@
M("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@
M("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E
M(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B
M.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@
M("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@
M("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@
M("`@("`@("`@(G%U97)Y(CH@(FQE="!T;W1A;$-053UT;W-C86QA<BA<;DMU
M8F5.;V1E4W1A='5S0V%P86-I='E<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4
M:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W
M:&5R92!,86)E;',N<F5S;W5R8V4@/3T@7")C<'5<(EQN?"!W:&5R92!,86)E
M;',N=6YI="`]/2!<(F-O<F5<(EQN?"!E>'1E;F0@;F]D93UT;W-T<FEN9RA,
M86)E;',N;F]D92E<;GP@9&ES=&EN8W0@;F]D92P@5F%L=65<;GP@<W5M;6%R
M:7IE(%9A;'5E/7-U;2A686QU92DI.UQN0V]N=&%I;F5R0W!U57-A9V5396-O
M;F1S5&]T86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\
M('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W:&5R92!,86)E;',N
M8W!U/3U<(G1O=&%L7")<;GP@=VAE<F4@0V]N=&%I;F5R(#T](%PB8V%D=FES
M;W)<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG*$QA8F5L<RYN86UE
M<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B96QS+G!O9"E<;GP@
M97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E;',N8V]N=&%I;F5R*5QN
M?"!W:&5R92!#;VYT86EN97(@/3T@7")<(EQN?"!E>'1E;F0@260@/2!T;W-T
M<FEN9RA,86)E;',N:60I7&Y\('=H97)E($ED(&5N9'-W:71H(%PB+G-L:6-E
M7")<;GP@:6YV;VME('!R;VU?<F%T92@I7&Y\('-U;6UA<FEZ92!686QU93UR
M;W5N9"AA=F<H5F%L=64I*S`N,#`P-2PS*2!B>2!B:6XH5&EM97-T86UP+"`V
M,#`P,&US*2P@3F%M97-P86-E+"!)9%QN?"!S=6UM87)I>F4@5F%L=64]<W5M
M*%9A;'5E*2!B>2!B:6XH5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI7&Y\
M('-U;6UA<FEZ92!296%L/6UA>"A686QU92DO=&]T86Q#4%5<;EQN(BP-"B`@
M("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E
M<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*
M("`@("`@("`@(")R969)9"(Z(")5<V%G92(L#0H@("`@("`@("`@(G)E<W5L
M=$9O<FUA="(Z(")T86)L92(-"B`@("`@("`@?2P-"B`@("`@("`@>PT*("`@
M("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B
M.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@
M("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M
M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C
M93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N
M(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@
M(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A
M;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-
M"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@
M("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=
M+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B:&ED92(Z(&9A;'-E+`T*("`@
M("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@("`@("`@("`B
M<75E<GDB.B`B;&5T('1O=&%L0U!5/71O<V-A;&%R*%QN2W5B94YO9&53=&%T
M=7-#87!A8VET>5QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<
M;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\('=H97)E($QA8F5L
M<RYR97-O=7)C92`]/2!<(F-P=5PB7&Y\('=H97)E($QA8F5L<RYU;FET(#T]
M(%PB8V]R95PB7&Y\(&5X=&5N9"!N;V1E/71O<W1R:6YG*$QA8F5L<RYN;V1E
M*5QN?"!D:7-T:6YC="!N;V1E+"!686QU95QN?"!S=6UM87)I>F4@5F%L=64]
M<W5M*%9A;'5E*2D[7&Y+=6)E4&]D0V]N=&%I;F5R4F5S;W5R8V5297%U97-T
M<UQN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@
M0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\('=H97)E($QA8F5L<RYR97-O=7)C
M93T]7")C<'5<(EQN?"!E>'1E;F0@=6ED/71O<W1R:6YG*$QA8F5L<RYU:60I
M7&Y\('-U;6UA<FEZ92!686QU93UM87@H5F%L=64I(&)Y(&)I;BA4:6UE<W1A
M;7`L(#%M*2P@=6ED7&Y\('-U;6UA<FEZ92!686QU93US=6TH5F%L=64I(&)Y
M(%1I;65S=&%M<%QN?"!S=6UM87)I>F4@5F%L=64]879G*%9A;'5E*5QN?"!P
M<F]J96-T(%)E<75E<W1S/59A;'5E+W1O=&%L0U!57&Y<;B(L#0H@("`@("`@
M("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP
M92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@
M("`@("`B<F5F260B.B`B4F5Q=65S=',B+`T*("`@("`@("`@(")R97-U;'1&
M;W)M870B.B`B=&%B;&4B#0H@("`@("`@('TL#0H@("`@("`@('L-"B`@("`@
M("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F1A=&%B87-E(CH@
M(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@
M("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A
M=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z
M=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z
M('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B
M97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD
M(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![#0H@
M("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@
M("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-
M"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*
M("`@("`@("`@('TL#0H@("`@("`@("`@(FAI9&4B.B!F86QS92P-"B`@("`@
M("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U
M97)Y(CH@(FQE="!T;W1A;$-053UT;W-C86QA<BA<;DMU8F5.;V1E4W1A='5S
M0V%P86-I='E<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\
M('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W:&5R92!,86)E;',N
M<F5S;W5R8V4@/3T@7")C<'5<(EQN?"!W:&5R92!,86)E;',N=6YI="`]/2!<
M(F-O<F5<(EQN?"!E>'1E;F0@;F]D93UT;W-T<FEN9RA,86)E;',N;F]D92E<
M;GP@9&ES=&EN8W0@;F]D92P@5F%L=65<;GP@<W5M;6%R:7IE(%9A;'5E/7-U
M;2A686QU92DI.UQN2W5B95!O9$-O;G1A:6YE<E)E<V]U<F-E3&EM:71S7&Y\
M('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S
M=&5R/3U<(B1#;'5S=&5R7")<;GP@=VAE<F4@3&%B96QS+G)E<V]U<F-E/3U<
M(F-P=5PB7&Y\(&5X=&5N9"!U:60]=&]S=')I;F<H3&%B96QS+G5I9"E<;GP@
M<W5M;6%R:7IE(%9A;'5E/6UA>"A686QU92D@8GD@8FEN*%1I;65S=&%M<"P@
M,6TI+"!U:61<;GP@<W5M;6%R:7IE(%9A;'5E/7-U;2A686QU92D@8GD@5&EM
M97-T86UP7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I7&Y\('!R;VIE
M8W0@3&EM:71S/59A;'5E+W1O=&%L0U!57&Y<;B(L#0H@("`@("`@("`@(G%U
M97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+
M44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B
M<F5F260B.B`B3&EM:71S(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@
M(G1A8FQE(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1I=&QE(CH@
M(D=L;V)A;"!#4%4@57-A9V4B+`T*("`@("`@(G1Y<&4B.B`B8F%R9V%U9V4B
M#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@
M("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S
M;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-
M"B`@("`@('TL#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@("`@(")D
M969A=6QT<R(Z('L-"B`@("`@("`@("`B8V]L;W(B.B![#0H@("`@("`@("`@
M("`B;6]D92(Z(")C;VYT:6YU;W5S+4=R66Q29"(-"B`@("`@("`@("!]+`T*
M("`@("`@("`@(")F:65L9$UI;DUA>"(Z(&9A;'-E+`T*("`@("`@("`@(")M
M87!P:6YG<R(Z(%M=+`T*("`@("`@("`@(")M87@B.B`Q+`T*("`@("`@("`@
M(")T:')E<VAO;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B86)S;VQU
M=&4B+`T*("`@("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@("`@("![
M#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@
M("`@("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@('TL#0H@("`@
M("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B<F5D(BP-
M"B`@("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*("`@("`@("`@("`@("!]
M#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B=6YI
M="(Z(")P97)C96YT=6YI="(-"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R
M:61E<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@
M("`@(")H(CH@."P-"B`@("`@("`@(G<B.B`V+`T*("`@("`@("`B>"(Z(#8L
M#0H@("`@("`@(")Y(CH@,`T*("`@("`@?2P-"B`@("`@(")I9"(Z(#4L#0H@
M("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(F1I<W!L87E-;V1E(CH@(FQC
M9"(L#0H@("`@("`@(")M87A6:7I(96EG:'0B.B`S,#`L#0H@("`@("`@(")M
M:6Y6:7I(96EG:'0B.B`Q-BP-"B`@("`@("`@(FUI;E9I>E=I9'1H(CH@."P-
M"B`@("`@("`@(FYA;650;&%C96UE;G0B.B`B875T;R(L#0H@("`@("`@(")O
M<FEE;G1A=&EO;B(Z(")H;W)I>F]N=&%L(BP-"B`@("`@("`@(G)E9'5C94]P
M=&EO;G,B.B![#0H@("`@("`@("`@(F-A;&-S(CH@6PT*("`@("`@("`@("`@
M(FQA<W1.;W1.=6QL(@T*("`@("`@("`@(%TL#0H@("`@("`@("`@(F9I96QD
M<R(Z("(B+`T*("`@("`@("`@(")V86QU97,B.B!F86QS90T*("`@("`@("!]
M+`T*("`@("`@("`B<VAO=U5N9FEL;&5D(CH@=')U92P-"B`@("`@("`@(G-I
M>FEN9R(Z(")A=71O(BP-"B`@("`@("`@(G9A;'5E36]D92(Z(")C;VQO<B(-
M"B`@("`@('TL#0H@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(Q,"XT+C$Q(BP-
M"B`@("`@(")T87)G971S(CH@6PT*("`@("`@("![#0H@("`@("`@("`@(D]P
M96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S
M(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T
M>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E
M(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*
M("`@("`@("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@
M("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I
M;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@
M("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B
M.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z
M('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@
M("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@
M("!]+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@
M("`@("`@("`B<75E<GDB.B`B;&5T('1O=&%L365M/71O<V-A;&%R*%QN2W5B
M94YO9&53=&%T=7-#87!A8VET>5QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I
M;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\('=H
M97)E($QA8F5L<RYR97-O=7)C92`]/2!<(FUE;6]R>5PB7&Y\('=H97)E($QA
M8F5L<RYU;FET(#T](%PB8GET95PB7&Y\(&5X=&5N9"!N;V1E/71O<W1R:6YG
M*$QA8F5L<RYN;V1E*5QN?"!D:7-T:6YC="!N;V1E+"!686QU95QN?"!S=6UM
M87)I>F4@5F%L=64]<W5M*%9A;'5E*2D[7&Y#;VYT86EN97)-96UO<GE7;W)K
M:6YG4V5T0GET97-<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I
M7&Y\('=H97)E($QA8F5L<R`A:&%S(%PB:61<(EQN?"!W:&5R92!#;'5S=&5R
M/3U<(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT;W-T<FEN9RA,
M86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R:6YG*$QA8F5L
M<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H3&%B96QS+F-O
M;G1A:6YE<BE<;GP@=VAE<F4@0V]N=&%I;F5R("$](%PB7")<;GP@97AT96YD
M(%9A;'5E/59A;'5E7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I(&)Y
M(&)I;BA4:6UE<W1A;7`L(#%M*2P@3F%M97-P86-E+"!0;V0L($-O;G1A:6YE
M<EQN?"!S=6UM87)I>F4@5F%L=64]<W5M*%9A;'5E*2!B>2!B:6XH5&EM97-T
M86UP+"`Q;2DL($YA;65S<&%C92P@4&]D7&Y\('-U;6UA<FEZ92!686QU93US
M=6TH5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L(#%M*5QN?"!S=6UM87)I>F4@
M57-E9#UM87@H5F%L=64I7&Y\('!R;VIE8W0@4F5A;#U5<V5D+W1O=&%L365M
M7&Y<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@
M("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E
M(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B57-A9V4B+`T*("`@("`@
M("`@(")R97-U;'1&;W)M870B.B`B=&%B;&4B#0H@("`@("`@('TL#0H@("`@
M("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@
M(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E
M(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A
M+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D
M>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B
M97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@
M("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@
M(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R
M961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*
M("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*
M("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S
M<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@
M("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(FAI9&4B.B!F
M86QS92P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@
M("`@("`@("`@(G%U97)Y(CH@(FQE="!T;W1A;$UE;3UT;W-C86QA<BA<;DMU
M8F5.;V1E4W1A='5S0V%P86-I='E<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4
M:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W
M:&5R92!,86)E;',N<F5S;W5R8V4@/3T@7")M96UO<GE<(EQN?"!W:&5R92!,
M86)E;',N=6YI="`]/2!<(F)Y=&5<(EQN?"!E>'1E;F0@;F]D93UT;W-T<FEN
M9RA,86)E;',N;F]D92E<;GP@9&ES=&EN8W0@;F]D92P@5F%L=65<;GP@<W5M
M;6%R:7IE(%9A;'5E/7-U;2A686QU92DI.UQN2W5B95!O9$-O;G1A:6YE<E)E
M<V]U<F-E4F5Q=65S='-<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A
M;7`I7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W:&5R92!,
M86)E;',N<F5S;W5R8V4]/5PB;65M;W)Y7")<;GP@97AT96YD('5I9#UT;W-T
M<FEN9RA,86)E;',N=6ED*5QN?"!S=6UM87)I>F4@5F%L=64];6%X*%9A;'5E
M*2!B>2!B:6XH5&EM97-T86UP+"`Q;2DL('5I9%QN?"!S=6UM87)I>F4@5F%L
M=64]<W5M*%9A;'5E*2!B>2!4:6UE<W1A;7!<;GP@<W5M;6%R:7IE(%9A;'5E
M/6%V9RA686QU92E<;GP@<')O:F5C="!297%U97-T<SU686QU92]T;W1A;$UE
M;5QN(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@
M("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B
M.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")297%U97-T<R(L#0H@("`@
M("`@("`@(G)E<W5L=$9O<FUA="(Z(")T86)L92(-"B`@("`@("`@?2P-"B`@
M("`@("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@
M("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R
M8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A
M=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@
M(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@
M(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@
M("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@
M("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B:&ED92(Z
M(&9A;'-E+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N,"XS(BP-
M"B`@("`@("`@("`B<75E<GDB.B`B;&5T('1O=&%L365M/71O<V-A;&%R*%QN
M2W5B94YO9&53=&%T=7-#87!A8VET>5QN?"!W:&5R92`D7U]T:6UE1FEL=&5R
M*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\
M('=H97)E($QA8F5L<RYR97-O=7)C92`]/2!<(FUE;6]R>5PB7&Y\('=H97)E
M($QA8F5L<RYU;FET(#T](%PB8GET95PB7&Y\(&5X=&5N9"!N;V1E/71O<W1R
M:6YG*$QA8F5L<RYN;V1E*5QN?"!D:7-T:6YC="!N;V1E+"!686QU95QN?"!S
M=6UM87)I>F4@5F%L=64]<W5M*%9A;'5E*2D[7&Y+=6)E4&]D0V]N=&%I;F5R
M4F5S;W5R8V5,:6UI='-<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A
M;7`I7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W:&5R92!,
M86)E;',N<F5S;W5R8V4]/5PB;65M;W)Y7")<;GP@97AT96YD('5I9#UT;W-T
M<FEN9RA,86)E;',N=6ED*5QN?"!S=6UM87)I>F4@5F%L=64];6%X*%9A;'5E
M*2!B>2!B:6XH5&EM97-T86UP+"`Q;2DL('5I9%QN?"!S=6UM87)I>F4@5F%L
M=64]<W5M*%9A;'5E*2!B>2!4:6UE<W1A;7!<;GP@<W5M;6%R:7IE(%9A;'5E
M/6%V9RA686QU92E<;GP@<')O:F5C="!,:6UI=',]5F%L=64O=&]T86Q-96U<
M;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@
M("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@
M=')U92P-"B`@("`@("`@("`B<F5F260B.B`B3&EM:71S(BP-"B`@("`@("`@
M("`B<F5S=6QT1F]R;6%T(CH@(G1A8FQE(@T*("`@("`@("!]#0H@("`@("!=
M+`T*("`@("`@(G1I=&QE(CH@(D=L;V)A;"!204T@57-A9V4B+`T*("`@("`@
M(G1Y<&4B.B`B8F%R9V%U9V4B#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T
M87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD
M871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[
M1&%T87-O=7)C93IT97AT?2(-"B`@("`@('TL#0H@("`@("`B9FEE;&1#;VYF
M:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z('L-"B`@("`@("`@("`B8V]L
M;W(B.B![#0H@("`@("`@("`@("`B9FEX961#;VQO<B(Z(")G<F5E;B(L#0H@
M("`@("`@("`@("`B;6]D92(Z(")F:7AE9"(-"B`@("`@("`@("!]+`T*("`@
M("`@("`@(")M87!P:6YG<R(Z(%M=+`T*("`@("`@("`@(")M87@B.B`Q,#`P
M+`T*("`@("`@("`@(")M:6XB.B`P+`T*("`@("`@("`@(")T:')E<VAO;&1S
M(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B86)S;VQU=&4B+`T*("`@("`@
M("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@("`@("`@
M("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@("`@("`@("`B=F%L
M=64B.B!N=6QL#0H@("`@("`@("`@("`@('TL#0H@("`@("`@("`@("`@('L-
M"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B<F5D(BP-"B`@("`@("`@("`@
M("`@("`B=F%L=64B.B`X,`T*("`@("`@("`@("`@("!]#0H@("`@("`@("`@
M("!=#0H@("`@("`@("`@?0T*("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I
M9&5S(CH@6UT-"B`@("`@('TL#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@
M("`@(F@B.B`T+`T*("`@("`@("`B=R(Z(#(L#0H@("`@("`@(")X(CH@,3(L
M#0H@("`@("`@(")Y(CH@,`T*("`@("`@?2P-"B`@("`@(")I9"(Z(#$L#0H@
M("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B.B![#0H@
M("`@("`@(")C;VQO<DUO9&4B.B`B=F%L=64B+`T*("`@("`@("`B9W)A<&A-
M;V1E(CH@(F%R96$B+`T*("`@("`@("`B:G5S=&EF>4UO9&4B.B`B875T;R(L
M#0H@("`@("`@(")O<FEE;G1A=&EO;B(Z(")A=71O(BP-"B`@("`@("`@(G)E
M9'5C94]P=&EO;G,B.B![#0H@("`@("`@("`@(F-A;&-S(CH@6PT*("`@("`@
M("`@("`@(FUA>"(-"B`@("`@("`@("!=+`T*("`@("`@("`@(")F:65L9',B
M.B`B(BP-"B`@("`@("`@("`B=F%L=65S(CH@9F%L<V4-"B`@("`@("`@?2P-
M"B`@("`@("`@(G-H;W=097)C96YT0VAA;F=E(CH@9F%L<V4L#0H@("`@("`@
M(")T97AT36]D92(Z(")A=71O(BP-"B`@("`@("`@(G=I9&5,87EO=70B.B!T
M<G5E#0H@("`@("!]+`T*("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B,3`N-"XQ
M,2(L#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@("`@
M(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T
M<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@
M("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O
M=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT
M?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*
M("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@
M("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T
M>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE
M<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@
M("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@
M("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L
M#0H@("`@("`@("`@(G%U97)Y(CH@(D%P:7-E<G9E<E-T;W)A9V5/8FIE8W1S
M7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!E>'1E;F0@
M4F5S;W5R8V4]=&]S=')I;F<H3&%B96QS+G)E<V]U<F-E*5QN?"!W:&5R92!2
M97-O=7)C92!I;B`H7")N;V1E<UPB*5QN?"!S=6UM87)I>F4@5F%L=64]879G
M*%9A;'5E*5QN(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-
M"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A
M=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")!(BP-"B`@("`@
M("`@("`B<F5S=6QT1F]R;6%T(CH@(G1A8FQE(@T*("`@("`@("!]#0H@("`@
M("!=+`T*("`@("`@(G1I=&QE(CH@(DYO9&5S(BP-"B`@("`@(")T>7!E(CH@
M(G-T870B#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-
M"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R
M+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT
M97AT?2(-"B`@("`@('TL#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@
M("`@(")D969A=6QT<R(Z('L-"B`@("`@("`@("`B8V]L;W(B.B![#0H@("`@
M("`@("`@("`B;6]D92(Z(")P86QE='1E+6-L87-S:6,B#0H@("`@("`@("`@
M?2P-"B`@("`@("`@("`B8W5S=&]M(CH@>PT*("`@("`@("`@("`@(F%X:7-"
M;W)D97)3:&]W(CH@9F%L<V4L#0H@("`@("`@("`@("`B87AI<T-E;G1E<F5D
M6F5R;R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X:7-#;VQO<DUO9&4B.B`B
M=&5X="(L#0H@("`@("`@("`@("`B87AI<TQA8F5L(CH@(B(L#0H@("`@("`@
M("`@("`B87AI<U!L86-E;65N="(Z(")A=71O(BP-"B`@("`@("`@("`@(")B
M87)!;&EG;FUE;G0B.B`P+`T*("`@("`@("`@("`@(F1R87=3='EL92(Z(")L
M:6YE(BP-"B`@("`@("`@("`@(")F:6QL3W!A8VET>2(Z(#(U+`T*("`@("`@
M("`@("`@(F=R861I96YT36]D92(Z(")O<&%C:71Y(BP-"B`@("`@("`@("`@
M(")H:61E1G)O;2(Z('L-"B`@("`@("`@("`@("`@(FQE9V5N9"(Z(&9A;'-E
M+`T*("`@("`@("`@("`@("`B=&]O;'1I<"(Z(&9A;'-E+`T*("`@("`@("`@
M("`@("`B=FEZ(CH@9F%L<V4-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`B:6YS97)T3G5L;',B.B!F86QS92P-"B`@("`@("`@("`@(")L:6YE26YT
M97)P;VQA=&EO;B(Z(")L:6YE87(B+`T*("`@("`@("`@("`@(FQI;F57:61T
M:"(Z(#(L#0H@("`@("`@("`@("`B<&]I;G13:7IE(CH@-2P-"B`@("`@("`@
M("`@(")S8V%L941I<W1R:6)U=&EO;B(Z('L-"B`@("`@("`@("`@("`@(G1Y
M<&4B.B`B;&EN96%R(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")S
M:&]W4&]I;G1S(CH@(FYE=F5R(BP-"B`@("`@("`@("`@(")S<&%N3G5L;',B
M.B!F86QS92P-"B`@("`@("`@("`@(")S=&%C:VEN9R(Z('L-"B`@("`@("`@
M("`@("`@(F=R;W5P(CH@(D$B+`T*("`@("`@("`@("`@("`B;6]D92(Z(")N
M;VYE(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")T:')E<VAO;&1S
M4W1Y;&4B.B![#0H@("`@("`@("`@("`@(")M;V1E(CH@(F]F9B(-"B`@("`@
M("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")M87!P:6YG<R(Z
M(%M=+`T*("`@("`@("`@(")T:')E<VAO;&1S(CH@>PT*("`@("`@("`@("`@
M(FUO9&4B.B`B86)S;VQU=&4B+`T*("`@("`@("`@("`@(G-T97!S(CH@6PT*
M("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(F=R
M965N(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B
M8V]L;W(B.B`B<F5D(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*
M("`@("`@("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?0T*
M("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6UT-"B`@("`@('TL
M#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`Q,BP-"B`@("`@
M("`@(G<B.B`Q,"P-"B`@("`@("`@(G@B.B`Q-"P-"B`@("`@("`@(GDB.B`P
M#0H@("`@("!]+`T*("`@("`@(FED(CH@,3(L#0H@("`@("`B:6YT97)V86PB
M.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@("`@(")L96=E;F0B
M.B![#0H@("`@("`@("`@(F-A;&-S(CH@6PT*("`@("`@("`@("`@(FUI;B(L
M#0H@("`@("`@("`@("`B;6%X(@T*("`@("`@("`@(%TL#0H@("`@("`@("`@
M(F1I<W!L87E-;V1E(CH@(G1A8FQE(BP-"B`@("`@("`@("`B<&QA8V5M96YT
M(CH@(G)I9VAT(BP-"B`@("`@("`@("`B<VAO=TQE9V5N9"(Z('1R=64-"B`@
M("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@("`@(FUO
M9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R="(Z(")N;VYE(@T*("`@
M("`@("!]#0H@("`@("!]+`T*("`@("`@(G1A<F=E=',B.B!;#0H@("`@("`@
M('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F1A
M=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@
M>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X
M<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A
M=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP
M<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@
M("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y
M<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U
M8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@
M("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@
M("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO
M;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@
M("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(G!L=6=I;E9E<G-I
M;VXB.B`B-2XP+C4B+`T*("`@("`@("`@(")Q=65R>2(Z(")!<&ES97)V97)3
M=&]R86=E3V)J96-T<UQN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M
M<"E<;GP@97AT96YD(%)E<V]U<F-E/71O<W1R:6YG*$QA8F5L<RYR97-O=7)C
M92E<;GP@=VAE<F4@4F5S;W5R8V4@:6X@*%PB8V]N9FEG;6%P<UPB+"!<(G!O
M9'-<(BP@7")D865M;VYS971S+F%P<'-<(BP@7")D97!L;WEM96YT+F%P<'-<
M(BP@7")E=F5N='-<(BP@7")E;F1P;VEN='-<(BP@7")S=&%T969U;'-E=',N
M87!P<UPB+"!<(G-E8W)E='-<(BP@7")N;V1E<UPB+"!<(FYA;65S<&%C97-<
M(BE<;GP@97AT96YD(%)E<V]U<F-E/7)E<&QA8V5?<W1R:6YG*%)E<V]U<F-E
M+"!<(BYA<'!S7"(L(%PB7"(I7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L
M=64I(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL(%)E<V]U
M<F-E7&Y\('!R;VIE8W0@5&EM97-T86UP+"!297-O=7)C92P@5F%L=65<;GP@
M;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B<75E<GE3
M;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L
M#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)
M9"(Z(")!(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?<V5R
M:65S(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1I=&QE(CH@(DMU
M8F5R;F5T97,@4F5S;W5R8V4@0V]U;G0B+`T*("`@("`@(G1Y<&4B.B`B=&EM
M97-E<FEE<R(-"B`@("!]+`T*("`@('L-"B`@("`@(")D871A<V]U<F-E(CH@
M>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R
M97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E
M.G1E>'1](@T*("`@("`@?2P-"B`@("`@(")F:65L9$-O;F9I9R(Z('L-"B`@
M("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@(")C;VQO<B(Z('L-"B`@
M("`@("`@("`@(")F:7AE9$-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@("`@
M(")M;V1E(CH@(F9I>&5D(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(FUA
M<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(FUI;B(Z(#`L#0H@("`@("`@("`@
M(G1H<F5S:&]L9',B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")A8G-O;'5T
M92(L#0H@("`@("`@("`@("`B<W1E<',B.B!;#0H@("`@("`@("`@("`@('L-
M"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@
M("`@("`@(")V86QU92(Z(&YU;&P-"B`@("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")R960B+`T*
M("`@("`@("`@("`@("`@(")V86QU92(Z(#@P#0H@("`@("`@("`@("`@('T-
M"B`@("`@("`@("`@(%T-"B`@("`@("`@("!]#0H@("`@("`@('TL#0H@("`@
M("`@(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S
M(CH@>PT*("`@("`@("`B:"(Z(#0L#0H@("`@("`@(")W(CH@,BP-"B`@("`@
M("`@(G@B.B`Q,BP-"B`@("`@("`@(GDB.B`T#0H@("`@("!]+`T*("`@("`@
M(FED(CH@,3,L#0H@("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P
M=&EO;G,B.B![#0H@("`@("`@(")C;VQO<DUO9&4B.B`B=F%L=64B+`T*("`@
M("`@("`B9W)A<&A-;V1E(CH@(FYO;F4B+`T*("`@("`@("`B:G5S=&EF>4UO
M9&4B.B`B875T;R(L#0H@("`@("`@(")O<FEE;G1A=&EO;B(Z(")A=71O(BP-
M"B`@("`@("`@(G)E9'5C94]P=&EO;G,B.B![#0H@("`@("`@("`@(F-A;&-S
M(CH@6PT*("`@("`@("`@("`@(FQA<W1.;W1.=6QL(@T*("`@("`@("`@(%TL
M#0H@("`@("`@("`@(F9I96QD<R(Z("(B+`T*("`@("`@("`@(")V86QU97,B
M.B!F86QS90T*("`@("`@("!]+`T*("`@("`@("`B<VAO=U!E<F-E;G1#:&%N
M9V4B.B!F86QS92P-"B`@("`@("`@(G1E>'1-;V1E(CH@(F%U=&\B+`T*("`@
M("`@("`B=VED94QA>6]U="(Z('1R=64-"B`@("`@('TL#0H@("`@("`B<&QU
M9VEN5F5R<VEO;B(Z("(Q,"XT+C$Q(BP-"B`@("`@(")T87)G971S(CH@6PT*
M("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@
M("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O
M=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M
M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B
M.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@("`@('TL#0H@("`@("`@
M("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-
M"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@
M("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;
M72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@
M?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X
M<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-
M"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G
M:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B07!I
M<V5R=F5R4W1O<F%G94]B:F5C='-<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4
M:6UE<W1A;7`I7&Y\(&5X=&5N9"!297-O=7)C93UT;W-T<FEN9RA,86)E;',N
M<F5S;W5R8V4I7&Y\('=H97)E(%)E<V]U<F-E(&EN("A<(FYA;65S<&%C97-<
M(BE<;GP@<W5M;6%R:7IE(%9A;'5E/6UA>"A686QU92E<;B(L#0H@("`@("`@
M("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP
M92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@
M("`@("`B<F5F260B.B`B02(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z
M(")T86)L92(-"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z
M(").86UE<W!A8V5S(BP-"B`@("`@(")T>7!E(CH@(G-T870B#0H@("`@?2P-
M"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B
M.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*
M("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@('TL
M#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z
M('L-"B`@("`@("`@("`B8V]L;W(B.B![#0H@("`@("`@("`@("`B9FEX961#
M;VQO<B(Z(")G<F5E;B(L#0H@("`@("`@("`@("`B;6]D92(Z(")F:7AE9"(-
M"B`@("`@("`@("!]+`T*("`@("`@("`@(")M87!P:6YG<R(Z(%M=+`T*("`@
M("`@("`@(")T:')E<VAO;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B
M86)S;VQU=&4B+`T*("`@("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@
M("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@
M("`@("`@("`@("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B
M<F5D(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*("`@("`@("`@
M("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@("`@
M("`B=6YI="(Z(")N;VYE(@T*("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I
M9&5S(CH@6PT*("`@("`@("`@('L-"B`@("`@("`@("`@(")M871C:&5R(CH@
M>PT*("`@("`@("`@("`@("`B:60B.B`B8GE.86UE(BP-"B`@("`@("`@("`@
M("`@(F]P=&EO;G,B.B`B4F5A;"(-"B`@("`@("`@("`@('TL#0H@("`@("`@
M("`@("`B<')O<&5R=&EE<R(Z(%L-"B`@("`@("`@("`@("`@>PT*("`@("`@
M("`@("`@("`@(")I9"(Z(")D96-I;6%L<R(L#0H@("`@("`@("`@("`@("`@
M(G9A;'5E(CH@,@T*("`@("`@("`@("`@("!]#0H@("`@("`@("`@("!=#0H@
M("`@("`@("`@?0T*("`@("`@("!=#0H@("`@("!]+`T*("`@("`@(F=R:610
M;W,B.B![#0H@("`@("`@(")H(CH@-"P-"B`@("`@("`@(G<B.B`V+`T*("`@
M("`@("`B>"(Z(#`L#0H@("`@("`@(")Y(CH@.`T*("`@("`@?2P-"B`@("`@
M(")I9"(Z(#8L#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(F-O;&]R
M36]D92(Z(")V86QU92(L#0H@("`@("`@(")G<F%P:$UO9&4B.B`B;F]N92(L
M#0H@("`@("`@(")J=7-T:69Y36]D92(Z(")A=71O(BP-"B`@("`@("`@(F]R
M:65N=&%T:6]N(CH@(F%U=&\B+`T*("`@("`@("`B<F5D=6-E3W!T:6]N<R(Z
M('L-"B`@("`@("`@("`B8V%L8W,B.B!;#0H@("`@("`@("`@("`B;&%S=$YO
M=$YU;&PB#0H@("`@("`@("`@72P-"B`@("`@("`@("`B9FEE;&1S(CH@(B(L
M#0H@("`@("`@("`@(G9A;'5E<R(Z(&9A;'-E#0H@("`@("`@('TL#0H@("`@
M("`@(")S:&]W4&5R8V5N=$-H86YG92(Z(&9A;'-E+`T*("`@("`@("`B=&5X
M="(Z('M]+`T*("`@("`@("`B=&5X=$UO9&4B.B`B=F%L=65?86YD7VYA;64B
M+`T*("`@("`@("`B=VED94QA>6]U="(Z('1R=64-"B`@("`@('TL#0H@("`@
M("`B<&QU9VEN5F5R<VEO;B(Z("(Q,"XT+C$Q(BP-"B`@("`@(")T87)G971S
M(CH@6PT*("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*
M("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B
M9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M
M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@
M(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!"
M>2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@
M("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@
M("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO
M;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@
M("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@
M("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@
M(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@
M(")P;'5G:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@("`@("`@("`B<75E<GDB
M.B`B0V]N=&%I;F5R0W!U57-A9V5396-O;F1S5&]T86Q<;GP@=VAE<F4@)%]?
M=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T97(]/5PB)$-L
M=7-T97)<(EQN?"!W:&5R92!,86)E;',N8W!U/3U<(G1O=&%L7")<;GP@=VAE
M<F4@0V]N=&%I;F5R(#T](%PB8V%D=FES;W)<(EQN?"!E>'1E;F0@3F%M97-P
M86-E/71O<W1R:6YG*$QA8F5L<RYN86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]
M=&]S=')I;F<H3&%B96QS+G!O9"E<;GP@97AT96YD($-O;G1A:6YE<CUT;W-T
M<FEN9RA,86)E;',N8V]N=&%I;F5R*5QN?"!W:&5R92!#;VYT86EN97(@/3T@
M7")<(EQN?"!E>'1E;F0@260@/2!T;W-T<FEN9RA,86)E;',N:60I7&Y\('=H
M97)E($ED(&5N9'-W:71H(%PB+G-L:6-E7")<;GP@:6YV;VME('!R;VU?<F%T
M92@I7&Y\('-U;6UA<FEZ92!686QU93UR;W5N9"AA=F<H5F%L=64I*S`N,#`P
M-2PQ*2!B>2!B:6XH5&EM97-T86UP+"`V,#`P,&US*2P@3F%M97-P86-E+"!)
M9%QN?"!S=6UM87)I>F4@5F%L=64]<W5M*%9A;'5E*2!B>2!B:6XH5&EM97-T
M86UP+"`D7U]T:6UE26YT97)V86PI7&Y\('-U;6UA<FEZ92!296%L/7)O=6YD
M*&UA>"A686QU92DL,2E<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@
M(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@
M("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B4F5A
M;"(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T86)L92(-"B`@("`@
M("`@?2P-"B`@("`@("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS92P-
M"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@
M(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA
M+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@
M("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*
M("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P
M0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@
M("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@
M("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I
M;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@
M("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z
M(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@
M("`B:&ED92(Z(&9A;'-E+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@
M(C4N,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B2W5B95!O9$-O;G1A:6YE
M<E)E<V]U<F-E4F5Q=65S='-<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE
M<W1A;7`I7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W:&5R
M92!,86)E;',N<F5S;W5R8V4]/5PB8W!U7")<;GP@97AT96YD('5I9#UT;W-T
M<FEN9RA,86)E;',N=6ED*5QN?"!S=6UM87)I>F4@5F%L=64];6%X*%9A;'5E
M*2!B>2!B:6XH5&EM97-T86UP+"`Q;2DL('5I9%QN?"!S=6UM87)I>F4@5F%L
M=64]<W5M*%9A;'5E*2!B>2!4:6UE<W1A;7!<;GP@<W5M;6%R:7IE(%9A;'5E
M/7)O=6YD*&%V9RA686QU92DL,2E<;GP@<')O:F5C="!297%U97-T<SU686QU
M92(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@
M("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@
M=')U92P-"B`@("`@("`@("`B<F5F260B.B`B4F5Q=65S=',B+`T*("`@("`@
M("`@(")R97-U;'1&;W)M870B.B`B=&%B;&4B#0H@("`@("`@('TL#0H@("`@
M("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@
M(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E
M(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A
M+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z(")A
M9'5X;')J;V$Y-79K8R(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R
M97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@
M("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP
M92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C
M92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@
M("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@
M("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N
M<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@
M("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B:&ED92(Z(&9A;'-E
M+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@("`@
M("`@("`B<75E<GDB.B`B2W5B95!O9$-O;G1A:6YE<E)E<V]U<F-E3&EM:71S
M7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#
M;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@=VAE<F4@3&%B96QS+G)E<V]U<F-E
M/3U<(F-P=5PB7&Y\(&5X=&5N9"!U:60]=&]S=')I;F<H3&%B96QS+G5I9"E<
M;GP@<W5M;6%R:7IE(%9A;'5E/6UA>"A686QU92D@8GD@8FEN*%1I;65S=&%M
M<"P@,6TI+"!U:61<;GP@<W5M;6%R:7IE(%9A;'5E/7-U;2A686QU92D@8GD@
M5&EM97-T86UP7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I7&Y\('!R
M;VIE8W0@3&EM:71S/59A;'5E(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B
M.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@
M("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z("),
M:6UI=',B+`T*("`@("`@("`@(")R97-U;'1&;W)M870B.B`B=&%B;&4B#0H@
M("`@("`@('TL#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L
M<V4L#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@
M("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A
M9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@
M("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@
M?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G
M<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-
M"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@
M("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y
M<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@
M("`@("`@(FAI9&4B.B!F86QS92P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO
M;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(DMU8F5.;V1E4W1A
M='5S0V%P86-I='E<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I
M7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W:&5R92!,86)E
M;',N<F5S;W5R8V4@/3T@7")C<'5<(EQN?"!W:&5R92!,86)E;',N=6YI="`]
M/2!<(F-O<F5<(EQN?"!E>'1E;F0@;F]D93UT;W-T<FEN9RA,86)E;',N;F]D
M92E<;GP@9&ES=&EN8W0@;F]D92P@5F%L=65<;GP@<W5M;6%R:7IE(%1O=&%L
M/7-U;2A686QU92DB+`T*("`@("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B
M+`T*("`@("`@("`@(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B
M<F%W36]D92(Z('1R=64L#0H@("`@("`@("`@(G)E9DED(CH@(E1O=&%L(BP-
M"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1A8FQE(@T*("`@("`@("!]
M#0H@("`@("!=+`T*("`@("`@(G1I=&QE(CH@(D-052!5<V%G92`H0V]R97,I
M(BP-"B`@("`@(")T>7!E(CH@(G-T870B#0H@("`@?2P-"B`@("![#0H@("`@
M("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA
M>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED
M(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@('TL#0H@("`@("`B9FEE
M;&1#;VYF:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z('L-"B`@("`@("`@
M("`B8V]L;W(B.B![#0H@("`@("`@("`@("`B9FEX961#;VQO<B(Z(")G<F5E
M;B(L#0H@("`@("`@("`@("`B;6]D92(Z(")F:7AE9"(-"B`@("`@("`@("!]
M+`T*("`@("`@("`@(")M87!P:6YG<R(Z(%M=+`T*("`@("`@("`@(")T:')E
M<VAO;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B86)S;VQU=&4B+`T*
M("`@("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@
M("`@("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@("`@("`@
M("`B=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@
M(%T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")U;FET(CH@(F1E8V)Y=&5S
M(@T*("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6UT-"B`@("`@
M('TL#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`T+`T*("`@
M("`@("`B=R(Z(#8L#0H@("`@("`@(")X(CH@-BP-"B`@("`@("`@(GDB.B`X
M#0H@("`@("!]+`T*("`@("`@(FED(CH@-RP-"B`@("`@(")O<'1I;VYS(CH@
M>PT*("`@("`@("`B8V]L;W)-;V1E(CH@(G9A;'5E(BP-"B`@("`@("`@(F=R
M87!H36]D92(Z(")N;VYE(BP-"B`@("`@("`@(FIU<W1I9GE-;V1E(CH@(F%U
M=&\B+`T*("`@("`@("`B;W)I96YT871I;VXB.B`B875T;R(L#0H@("`@("`@
M(")R961U8V5/<'1I;VYS(CH@>PT*("`@("`@("`@(")C86QC<R(Z(%L-"B`@
M("`@("`@("`@(")L87-T3F]T3G5L;"(-"B`@("`@("`@("!=+`T*("`@("`@
M("`@(")F:65L9',B.B`B(BP-"B`@("`@("`@("`B=F%L=65S(CH@9F%L<V4-
M"B`@("`@("`@?2P-"B`@("`@("`@(G-H;W=097)C96YT0VAA;F=E(CH@9F%L
M<V4L#0H@("`@("`@(")T97AT(CH@>WTL#0H@("`@("`@(")T97AT36]D92(Z
M(")V86QU95]A;F1?;F%M92(L#0H@("`@("`@(")W:61E3&%Y;W5T(CH@=')U
M90T*("`@("`@?2P-"B`@("`@(")P;'5G:6Y697)S:6]N(CH@(C$P+C0N,3$B
M+`T*("`@("`@(G1A<F=E=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B
M3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I
M8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@
M(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R
M8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@
M("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S
M<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@
M("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@
M("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP
M92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E
M(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@
M("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@
M("`@('TL#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*
M("`@("`@("`@(")Q=65R>2(Z(")#;VYT86EN97)-96UO<GE7;W)K:6YG4V5T
M0GET97-<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H
M97)E($QA8F5L<R`A:&%S(%PB:61<(EQN?"!W:&5R92!#;'5S=&5R/3U<(B1#
M;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT;W-T<FEN9RA,86)E;',N
M;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R:6YG*$QA8F5L<RYP;V0I
M7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H3&%B96QS+F-O;G1A:6YE
M<BE<;GP@=VAE<F4@0V]N=&%I;F5R("$](%PB7")<;GP@97AT96YD(%9A;'5E
M/59A;'5E7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I(&)Y(&)I;BA4
M:6UE<W1A;7`L(#%M*2P@3F%M97-P86-E+"!0;V0L($-O;G1A:6YE<EQN?"!S
M=6UM87)I>F4@5F%L=64]<W5M*%9A;'5E*2!B>2!B:6XH5&EM97-T86UP+"`Q
M;2DL($YA;65S<&%C92P@4&]D7&Y\('-U;6UA<FEZ92!686QU93US=6TH5F%L
M=64I(&)Y(&)I;BA4:6UE<W1A;7`L(#%M*5QN?"!S=6UM87)I>F4@57-E9#UM
M87@H5F%L=64I7&Y\('!R;VIE8W0@4F5A;#U5<V5D(BP-"B`@("`@("`@("`B
M<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@
M(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@
M(")R969)9"(Z(")296%L(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@
M(G1A8FQE(@T*("`@("`@("!]+`T*("`@("`@("![#0H@("`@("`@("`@(D]P
M96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S
M(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T
M>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E
M(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*
M("`@("`@("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@
M("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I
M;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@
M("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B
M.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z
M('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@
M("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@
M("!]+`T*("`@("`@("`@(")H:61E(CH@9F%L<V4L#0H@("`@("`@("`@(G!L
M=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R>2(Z(")+
M=6)E4&]D0V]N=&%I;F5R4F5S;W5R8V5297%U97-T<UQN?"!W:&5R92`D7U]T
M:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU
M<W1E<EPB7&Y\('=H97)E($QA8F5L<RYR97-O=7)C93T]7")M96UO<GE<(EQN
M?"!E>'1E;F0@=6ED/71O<W1R:6YG*$QA8F5L<RYU:60I7&Y\('-U;6UA<FEZ
M92!686QU93UM87@H5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L(#%M*2P@=6ED
M7&Y\('-U;6UA<FEZ92!686QU93US=6TH5F%L=64I(&)Y(%1I;65S=&%M<%QN
M?"!S=6UM87)I>F4@5F%L=64]879G*%9A;'5E*5QN?"!P<F]J96-T(%)E<75E
M<W1S/59A;'5E(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-
M"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A
M=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")297%U97-T<R(L
M#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T86)L92(-"B`@("`@("`@
M?2P-"B`@("`@("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@
M("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A
M=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z
M=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B
M=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@
M("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB
M.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@
M("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@
M("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS
M(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@
M("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@
M(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A
M;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B
M:&ED92(Z(&9A;'-E+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N
M,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B2W5B95!O9$-O;G1A:6YE<E)E
M<V]U<F-E3&EM:71S7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP
M*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@=VAE<F4@3&%B
M96QS+G)E<V]U<F-E/3U<(FUE;6]R>5PB7&Y\(&5X=&5N9"!U:60]=&]S=')I
M;F<H3&%B96QS+G5I9"E<;GP@<W5M;6%R:7IE(%9A;'5E/6UA>"A686QU92D@
M8GD@8FEN*%1I;65S=&%M<"P@,6TI+"!U:61<;GP@<W5M;6%R:7IE(%9A;'5E
M/7-U;2A686QU92D@8GD@5&EM97-T86UP7&Y\('-U;6UA<FEZ92!686QU93UA
M=F<H5F%L=64I7&Y\('!R;VIE8W0@3&EM:71S/59A;'5E(BP-"B`@("`@("`@
M("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E
M(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@
M("`@(")R969)9"(Z("),:6UI=',B+`T*("`@("`@("`@(")R97-U;'1&;W)M
M870B.B`B=&%B;&4B#0H@("`@("`@('TL#0H@("`@("`@('L-"B`@("`@("`@
M("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE
M=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@
M("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S
M;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X
M='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-
M"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP
M<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*
M("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@
M("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B
M='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H
M97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@
M("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@
M("`@("`@('TL#0H@("`@("`@("`@(FAI9&4B.B!F86QS92P-"B`@("`@("`@
M("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y
M(CH@(DMU8F5.;V1E4W1A='5S0V%P86-I='E<;GP@=VAE<F4@)%]?=&EM949I
M;'1E<BA4:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<
M(EQN?"!W:&5R92!,86)E;',N<F5S;W5R8V4@/3T@7")M96UO<GE<(EQN?"!W
M:&5R92!,86)E;',N=6YI="`]/2!<(F)Y=&5<(EQN?"!E>'1E;F0@;F]D93UT
M;W-T<FEN9RA,86)E;',N;F]D92E<;GP@9&ES=&EN8W0@;F]D92P@5F%L=65<
M;GP@<W5M;6%R:7IE(%1O=&%L/7-U;2A686QU92DB+`T*("`@("`@("`@(")Q
M=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R>51Y<&4B.B`B
M2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@("`@("`@("`@
M(G)E9DED(CH@(E1O=&%L(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@
M(G1A8FQE(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1I=&QE(CH@
M(E)!32!5<V%G92(L#0H@("`@("`B='EP92(Z(")S=&%T(@T*("`@('TL#0H@
M("`@>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@
M(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@
M("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("!]+`T*
M("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B.B![
M#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(F9I>&5D0V]L
M;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@(FUO9&4B.B`B9FEX960B#0H@
M("`@("`@("`@?2P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;72P-"B`@("`@
M("`@("`B;6EN(CH@,"P-"B`@("`@("`@("`B=&AR97-H;VQD<R(Z('L-"B`@
M("`@("`@("`@(")M;V1E(CH@(F%B<V]L=71E(BP-"B`@("`@("`@("`@(")S
M=&5P<R(Z(%L-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")C
M;VQO<B(Z(")G<F5E;B(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@;G5L
M;`T*("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@("![#0H@("`@("`@
M("`@("`@("`@(F-O;&]R(CH@(G)E9"(L#0H@("`@("`@("`@("`@("`@(G9A
M;'5E(CH@.#`-"B`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@
M("`@("`@('T-"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R:61E<R(Z(%M=
M#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@
M-"P-"B`@("`@("`@(G<B.B`R+`T*("`@("`@("`B>"(Z(#$R+`T*("`@("`@
M("`B>2(Z(#@-"B`@("`@('TL#0H@("`@("`B:60B.B`R+`T*("`@("`@(FEN
M=&5R=F%L(CH@(C%M(BP-"B`@("`@(")O<'1I;VYS(CH@>PT*("`@("`@("`B
M8V]L;W)-;V1E(CH@(G9A;'5E(BP-"B`@("`@("`@(F=R87!H36]D92(Z(")N
M;VYE(BP-"B`@("`@("`@(FIU<W1I9GE-;V1E(CH@(F%U=&\B+`T*("`@("`@
M("`B;W)I96YT871I;VXB.B`B875T;R(L#0H@("`@("`@(")R961U8V5/<'1I
M;VYS(CH@>PT*("`@("`@("`@(")C86QC<R(Z(%L-"B`@("`@("`@("`@(")L
M87-T3F]T3G5L;"(-"B`@("`@("`@("!=+`T*("`@("`@("`@(")F:65L9',B
M.B`B(BP-"B`@("`@("`@("`B=F%L=65S(CH@9F%L<V4-"B`@("`@("`@?2P-
M"B`@("`@("`@(G-H;W=097)C96YT0VAA;F=E(CH@9F%L<V4L#0H@("`@("`@
M(")T97AT36]D92(Z(")A=71O(BP-"B`@("`@("`@(G=I9&5,87EO=70B.B!T
M<G5E#0H@("`@("!]+`T*("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B,3`N-"XQ
M,2(L#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@("`@
M(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T
M<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@
M("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O
M=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT
M?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*
M("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@
M("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T
M>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE
M<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@
M("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@
M("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L
M#0H@("`@("`@("`@(G%U97)Y(CH@(D%P:7-E<G9E<E-T;W)A9V5/8FIE8W1S
M7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!E>'1E;F0@
M4F5S;W5R8V4]=&]S=')I;F<H3&%B96QS+G)E<V]U<F-E*5QN?"!W:&5R92!2
M97-O=7)C92!I;B`H7")P;V1S7"(I7&Y\('-U;6UA<FEZ92!686QU93UA=F<H
M5F%L=64I7&XB+`T*("`@("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*
M("`@("`@("`@(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W
M36]D92(Z('1R=64L#0H@("`@("`@("`@(G)E9DED(CH@(D$B+`T*("`@("`@
M("`@(")R97-U;'1&;W)M870B.B`B=&%B;&4B#0H@("`@("`@('T-"B`@("`@
M(%TL#0H@("`@("`B=&ET;&4B.B`B4&]D<R(L#0H@("`@("`B='EP92(Z(")S
M=&%T(@T*("`@('TL#0H@("`@>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@
M("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD
M871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X
M='TB#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@
M("`B9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@
M("`@("`@(FUO9&4B.B`B8V]N=&EN=6]U<RU'<EEL4F0B#0H@("`@("`@("`@
M?2P-"B`@("`@("`@("`B8W5S=&]M(CH@>PT*("`@("`@("`@("`@(F%X:7-"
M;W)D97)3:&]W(CH@9F%L<V4L#0H@("`@("`@("`@("`B87AI<T-E;G1E<F5D
M6F5R;R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X:7-#;VQO<DUO9&4B.B`B
M=&5X="(L#0H@("`@("`@("`@("`B87AI<TQA8F5L(CH@(B(L#0H@("`@("`@
M("`@("`B87AI<U!L86-E;65N="(Z(")A=71O(BP-"B`@("`@("`@("`@(")B
M87)!;&EG;FUE;G0B.B`P+`T*("`@("`@("`@("`@(F1R87=3='EL92(Z(")L
M:6YE(BP-"B`@("`@("`@("`@(")F:6QL3W!A8VET>2(Z(#$P+`T*("`@("`@
M("`@("`@(F=R861I96YT36]D92(Z(")N;VYE(BP-"B`@("`@("`@("`@(")H
M:61E1G)O;2(Z('L-"B`@("`@("`@("`@("`@(FQE9V5N9"(Z(&9A;'-E+`T*
M("`@("`@("`@("`@("`B=&]O;'1I<"(Z(&9A;'-E+`T*("`@("`@("`@("`@
M("`B=FEZ(CH@9F%L<V4-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M:6YS97)T3G5L;',B.B!F86QS92P-"B`@("`@("`@("`@(")L:6YE26YT97)P
M;VQA=&EO;B(Z(")L:6YE87(B+`T*("`@("`@("`@("`@(FQI;F57:61T:"(Z
M(#(L#0H@("`@("`@("`@("`B<&]I;G13:7IE(CH@-2P-"B`@("`@("`@("`@
M(")S8V%L941I<W1R:6)U=&EO;B(Z('L-"B`@("`@("`@("`@("`@(G1Y<&4B
M.B`B;&EN96%R(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")S:&]W
M4&]I;G1S(CH@(FYE=F5R(BP-"B`@("`@("`@("`@(")S<&%N3G5L;',B.B!F
M86QS92P-"B`@("`@("`@("`@(")S=&%C:VEN9R(Z('L-"B`@("`@("`@("`@
M("`@(F=R;W5P(CH@(D$B+`T*("`@("`@("`@("`@("`B;6]D92(Z(")N;VYE
M(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")T:')E<VAO;&1S4W1Y
M;&4B.B![#0H@("`@("`@("`@("`@(")M;V1E(CH@(F]F9B(-"B`@("`@("`@
M("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")M87!P:6YG<R(Z(%M=
M+`T*("`@("`@("`@(")M87@B.B`Q+`T*("`@("`@("`@(")M:6XB.B`P+`T*
M("`@("`@("`@(")T:')E<VAO;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B
M.B`B86)S;VQU=&4B+`T*("`@("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@
M("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-
M"B`@("`@("`@("`@("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@
M('TL#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B
M.B`B<F5D(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*("`@("`@
M("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@
M("`@("`B=6YI="(Z(")P97)C96YT=6YI="(-"B`@("`@("`@?2P-"B`@("`@
M("`@(F]V97)R:61E<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B
M.B![#0H@("`@("`@(")H(CH@-RP-"B`@("`@("`@(G<B.B`Q,BP-"B`@("`@
M("`@(G@B.B`P+`T*("`@("`@("`B>2(Z(#$R#0H@("`@("!]+`T*("`@("`@
M(FED(CH@,3$L#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(FQE9V5N
M9"(Z('L-"B`@("`@("`@("`B8V%L8W,B.B!;72P-"B`@("`@("`@("`B9&ES
M<&QA>4UO9&4B.B`B;&ES="(L#0H@("`@("`@("`@(G!L86-E;65N="(Z(")B
M;W1T;VTB+`T*("`@("`@("`@(")S:&]W3&5G96YD(CH@9F%L<V4-"B`@("`@
M("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@("`@(FUO9&4B
M.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R="(Z(")N;VYE(@T*("`@("`@
M("!]#0H@("`@("!]+`T*("`@("`@(G1A<F=E=',B.B!;#0H@("`@("`@('L-
M"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F1A=&%B
M87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*
M("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO
M<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S
M;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S
M<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@
M("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B
M.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B
M.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@
M("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@
M("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B
M.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@
M("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB
M.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R>2(Z(")L970@=&]T86Q#4%4]
M=&]S8V%L87(H7&Y+=6)E3F]D95-T871U<T-A<&%C:71Y7&Y\('=H97)E("1?
M7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#
M;'5S=&5R7")<;GP@=VAE<F4@3&%B96QS+G)E<V]U<F-E(#T](%PB8W!U7")<
M;GP@=VAE<F4@3&%B96QS+G5N:70@/3T@7")C;W)E7")<;GP@97AT96YD(&YO
M9&4]=&]S=')I;F<H3&%B96QS+FYO9&4I7&Y\(&1I<W1I;F-T(&YO9&4L(%9A
M;'5E7&Y\('-U;6UA<FEZ92!686QU93US=6TH5F%L=64I*3M<;D-O;G1A:6YE
M<D-P=55S86=E4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H
M5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@
M=VAE<F4@3&%B96QS+F-P=3T]7")T;W1A;%PB7&Y\(&5X=&5N9"!.86UE<W!A
M8V4]=&]S=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@97AT96YD(%!O9#UT
M;W-T<FEN9RA,86)E;',N<&]D*5QN?"!E>'1E;F0@0V]N=&%I;F5R/71O<W1R
M:6YG*$QA8F5L<RYC;VYT86EN97(I7&Y\('=H97)E($-O;G1A:6YE<B`A/2!<
M(EPB7&Y\(&EN=F]K92!P<F]M7V1E;'1A*"E<;GP@<W5M;6%R:7IE(%9A;'5E
M/7-U;2A686QU92DO-C`@8GD@8FEN*%1I;65S=&%M<"P@)%]?=&EM94EN=&5R
M=F%L*2P@3F%M97-P86-E7&Y\('!R;VIE8W0@5&EM97-T86UP+"!686QU92]T
M;W1A;$-055QN?"!O<F1E<B!B>2!4:6UE<W1A;7`@87-C7&Y<;EQN(BP-"B`@
M("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E
M<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*
M("`@("`@("`@(")R969)9"(Z(")5=&EL:7IA=&EO;B(L#0H@("`@("`@("`@
M(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@("`@?0T*("`@
M("`@72P-"B`@("`@(")T:71L92(Z(")#;'5S=&5R($-052!5=&EL:7IA=&EO
M;B(L#0H@("`@("`B='EP92(Z(")T:6UE<V5R:65S(@T*("`@('TL#0H@("`@
M>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R
M869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@
M("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("!]+`T*("`@
M("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@
M("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B8V]N
M=&EN=6]U<RU'<EEL4F0B#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B8W5S
M=&]M(CH@>PT*("`@("`@("`@("`@(F%X:7-";W)D97)3:&]W(CH@9F%L<V4L
M#0H@("`@("`@("`@("`B87AI<T-E;G1E<F5D6F5R;R(Z(&9A;'-E+`T*("`@
M("`@("`@("`@(F%X:7-#;VQO<DUO9&4B.B`B=&5X="(L#0H@("`@("`@("`@
M("`B87AI<TQA8F5L(CH@(B(L#0H@("`@("`@("`@("`B87AI<U!L86-E;65N
M="(Z(")A=71O(BP-"B`@("`@("`@("`@(")B87)!;&EG;FUE;G0B.B`P+`T*
M("`@("`@("`@("`@(F1R87=3='EL92(Z(")L:6YE(BP-"B`@("`@("`@("`@
M(")F:6QL3W!A8VET>2(Z(#$P+`T*("`@("`@("`@("`@(F=R861I96YT36]D
M92(Z(")N;VYE(BP-"B`@("`@("`@("`@(")H:61E1G)O;2(Z('L-"B`@("`@
M("`@("`@("`@(FQE9V5N9"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=&]O
M;'1I<"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=FEZ(CH@9F%L<V4-"B`@
M("`@("`@("`@('TL#0H@("`@("`@("`@("`B:6YS97)T3G5L;',B.B!F86QS
M92P-"B`@("`@("`@("`@(")L:6YE26YT97)P;VQA=&EO;B(Z(")L:6YE87(B
M+`T*("`@("`@("`@("`@(FQI;F57:61T:"(Z(#(L#0H@("`@("`@("`@("`B
M<&]I;G13:7IE(CH@-2P-"B`@("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO
M;B(Z('L-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@
M("`@("`@?2P-"B`@("`@("`@("`@(")S:&]W4&]I;G1S(CH@(FYE=F5R(BP-
M"B`@("`@("`@("`@(")S<&%N3G5L;',B.B!F86QS92P-"B`@("`@("`@("`@
M(")S=&%C:VEN9R(Z('L-"B`@("`@("`@("`@("`@(F=R;W5P(CH@(D$B+`T*
M("`@("`@("`@("`@("`B;6]D92(Z(")N;VYE(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")T:')E<VAO;&1S4W1Y;&4B.B![#0H@("`@("`@("`@
M("`@(")M;V1E(CH@(F]F9B(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]
M+`T*("`@("`@("`@(")M87!P:6YG<R(Z(%M=+`T*("`@("`@("`@(")M87@B
M.B`Q+`T*("`@("`@("`@(")M:6XB.B`P+`T*("`@("`@("`@(")T:')E<VAO
M;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B86)S;VQU=&4B+`T*("`@
M("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@("`@
M("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@("`@("`@("`B
M=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@('TL#0H@("`@("`@("`@("`@
M('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B<F5D(BP-"B`@("`@("`@
M("`@("`@("`B=F%L=64B.B`X,`T*("`@("`@("`@("`@("!]#0H@("`@("`@
M("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B=6YI="(Z(")P97)C
M96YT=6YI="(-"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R:61E<R(Z(%M=
M#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@
M-RP-"B`@("`@("`@(G<B.B`Q,BP-"B`@("`@("`@(G@B.B`Q,BP-"B`@("`@
M("`@(GDB.B`Q,@T*("`@("`@?2P-"B`@("`@(")I9"(Z(#$P+`T*("`@("`@
M(F]P=&EO;G,B.B![#0H@("`@("`@(")L96=E;F0B.B![#0H@("`@("`@("`@
M(F-A;&-S(CH@6UTL#0H@("`@("`@("`@(F1I<W!L87E-;V1E(CH@(FQI<W0B
M+`T*("`@("`@("`@(")P;&%C96UE;G0B.B`B8F]T=&]M(BP-"B`@("`@("`@
M("`B<VAO=TQE9V5N9"(Z(&9A;'-E#0H@("`@("`@('TL#0H@("`@("`@(")T
M;V]L=&EP(CH@>PT*("`@("`@("`@(")M;V1E(CH@(G-I;F=L92(L#0H@("`@
M("`@("`@(G-O<G0B.B`B;F]N92(-"B`@("`@("`@?0T*("`@("`@?2P-"B`@
M("`@(")T87)G971S(CH@6PT*("`@("`@("![#0H@("`@("`@("`@(D]P96Y!
M22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-
M"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E
M(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-
M"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@
M("`@("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@
M("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS
M(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@
M("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@
M("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B
M86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-
M"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@
M("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]
M+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@("`@
M("`@("`B<75E<GDB.B`B;&5T('1O=&%L365M/71O<V-A;&%R*%QN2W5B94YO
M9&53=&%T=7-#87!A8VET>5QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S
M=&%M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\('=H97)E
M($QA8F5L<RYR97-O=7)C92`]/2!<(FUE;6]R>5PB7&Y\('=H97)E($QA8F5L
M<RYU;FET(#T](%PB8GET95PB7&Y\(&5X=&5N9"!N;V1E/71O<W1R:6YG*$QA
M8F5L<RYN;V1E*5QN?"!D:7-T:6YC="!N;V1E+"!686QU95QN?"!S=6UM87)I
M>F4@5F%L=64]<W5M*%9A;'5E*2D[7&Y#;VYT86EN97)-96UO<GE5<V%G94)Y
M=&5S7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R
M92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT
M;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R
M:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H
M3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@0V]N=&%I;F5R("$](%PB7")<
M;GP@97AT96YD(%9A;'5E/59A;'5E7&Y\('-U;6UA<FEZ92!686QU93UA=F<H
M5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL($YA
M;65S<&%C92P@4&]D7&Y\('-U;6UA<FEZ92!686QU93US=6TH5F%L=64I(&)Y
M(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"E<;GP@<')O:F5C="!4
M:6UE<W1A;7`L(%9A;'5E/59A;'5E+W1O=&%L365M7&Y\(&]R9&5R(&)Y(%1I
M;65S=&%M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A
M=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@
M(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B571I;&EZ
M871I;VXB+`T*("`@("`@("`@(")R97-U;'1&;W)M870B.B`B=&EM95]S97)I
M97,B#0H@("`@("`@('T-"B`@("`@(%TL#0H@("`@("`B=&ET;&4B.B`B0VQU
M<W1E<B!-96UO<GD@571I;&EZ871I;VXB+`T*("`@("`@(G1Y<&4B.B`B=&EM
M97-E<FEE<R(-"B`@("!]+`T*("`@('L-"B`@("`@(")D871A<V]U<F-E(CH@
M>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R
M97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E
M.G1E>'1](@T*("`@("`@?2P-"B`@("`@(")F:65L9$-O;F9I9R(Z('L-"B`@
M("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@(")C;VQO<B(Z('L-"B`@
M("`@("`@("`@(")M;V1E(CH@(G!A;&5T=&4M8VQA<W-I8R(-"B`@("`@("`@
M("!]+`T*("`@("`@("`@(")C=7-T;VTB.B![#0H@("`@("`@("`@("`B87AI
M<T)O<F1E<E-H;W<B.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES0V5N=&5R
M961:97)O(CH@9F%L<V4L#0H@("`@("`@("`@("`B87AI<T-O;&]R36]D92(Z
M(")T97AT(BP-"B`@("`@("`@("`@(")A>&ES3&%B96PB.B`B(BP-"B`@("`@
M("`@("`@(")A>&ES4&QA8V5M96YT(CH@(F%U=&\B+`T*("`@("`@("`@("`@
M(F)A<D%L:6=N;65N="(Z(#`L#0H@("`@("`@("`@("`B9')A=U-T>6QE(CH@
M(FQI;F4B+`T*("`@("`@("`@("`@(F9I;&Q/<&%C:71Y(CH@,"P-"B`@("`@
M("`@("`@(")G<F%D:65N=$UO9&4B.B`B;F]N92(L#0H@("`@("`@("`@("`B
M:&ED949R;VTB.B![#0H@("`@("`@("`@("`@(")L96=E;F0B.B!F86QS92P-
M"B`@("`@("`@("`@("`@(G1O;VQT:7`B.B!F86QS92P-"B`@("`@("`@("`@
M("`@(G9I>B(Z(&9A;'-E#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(FEN<V5R=$YU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B;&EN94EN=&5R
M<&]L871I;VXB.B`B;&EN96%R(BP-"B`@("`@("`@("`@(")L:6YE5VED=&@B
M.B`Q+`T*("`@("`@("`@("`@(G!O:6YT4VEZ92(Z(#4L#0H@("`@("`@("`@
M("`B<V-A;&5$:7-T<FEB=71I;VXB.B![#0H@("`@("`@("`@("`@(")T>7!E
M(CH@(FQI;F5A<B(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<VAO
M=U!O:6YT<R(Z(")N979E<B(L#0H@("`@("`@("`@("`B<W!A;DYU;&QS(CH@
M9F%L<V4L#0H@("`@("`@("`@("`B<W1A8VMI;F<B.B![#0H@("`@("`@("`@
M("`@(")G<F]U<"(Z(")!(BP-"B`@("`@("`@("`@("`@(FUO9&4B.B`B;F]N
M92(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=&AR97-H;VQD<U-T
M>6QE(CH@>PT*("`@("`@("`@("`@("`B;6]D92(Z(")O9F8B#0H@("`@("`@
M("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B;&EN:W,B.B!;#0H@
M("`@("`@("`@("![#0H@("`@("`@("`@("`@(")T:71L92(Z(")6:65W($YA
M;65S<&%C92(L#0H@("`@("`@("`@("`@(")U<FPB.B`B:'1T<',Z+R]G<F%F
M86YA+6QA<F=E8VQU<W1E<BUD-&9F9W9D;F9T8G9C;69S+F-C82YG<F%F86YA
M+F%Z=7)E+F-O;2]D+V9D=F9J>G@Y;&]C=3AA+VYA;65S<&%C97,_;W)G260]
M,28D>U]?=7)L7W1I;65?<F%N9V5])B1[0VQU<W1E<CIQ=65R>7!A<F%M?29V
M87(M3F%M97-P86-E/21[7U]F:65L9"YL86)E;',N3F%M97-P86-E?2(-"B`@
M("`@("`@("`@('TL#0H@("`@("`@("`@("![#0H@("`@("`@("`@("`@(")T
M:71L92(Z(")6:65W(%!O9',B+`T*("`@("`@("`@("`@("`B=7)L(CH@(FAT
M='!S.B\O9W)A9F%N82UL87)G96-L=7-T97(M9#1F9F=V9&YF=&)V8VUF<RYC
M8V$N9W)A9F%N82YA>G5R92YC;VTO9"]C9'5X<#EN8FUJ,69K8B]P;V1S/V]R
M9TED/3$F<F5F<F5S:#TQ;3].86UE<W!A8V4])'M?7V9I96QD+FQA8F5L<RY.
M86UE<W!A8V5](@T*("`@("`@("`@("`@?0T*("`@("`@("`@(%TL#0H@("`@
M("`@("`@(FUA<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B
M.B![#0H@("`@("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@
M("`@("`B<W1E<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@
M("`@("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU
M92(Z(&YU;&P-"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@>PT*
M("`@("`@("`@("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@("`@
M("`@(")V86QU92(Z(#@P#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@
M(%T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")U;FET(CH@(D-O<F5S(@T*
M("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6UT-"B`@("`@('TL
M#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`X+`T*("`@("`@
M("`B=R(Z(#$R+`T*("`@("`@("`B>"(Z(#`L#0H@("`@("`@(")Y(CH@,3D-
M"B`@("`@('TL#0H@("`@("`B:60B.B`X+`T*("`@("`@(FEN=&5R=F%L(CH@
M(C%M(BP-"B`@("`@(")O<'1I;VYS(CH@>PT*("`@("`@("`B;&5G96YD(CH@
M>PT*("`@("`@("`@(")C86QC<R(Z(%M=+`T*("`@("`@("`@(")D:7-P;&%Y
M36]D92(Z(")L:7-T(BP-"B`@("`@("`@("`B<&QA8V5M96YT(CH@(F)O='1O
M;2(L#0H@("`@("`@("`@(G-H;W=,96=E;F0B.B!T<G5E#0H@("`@("`@('TL
M#0H@("`@("`@(")T;V]L=&EP(CH@>PT*("`@("`@("`@(")M;V1E(CH@(G-I
M;F=L92(L#0H@("`@("`@("`@(G-O<G0B.B`B;F]N92(-"B`@("`@("`@?0T*
M("`@("`@?2P-"B`@("`@(")T87)G971S(CH@6PT*("`@("`@("![#0H@("`@
M("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z
M(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@
M("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD
M871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E
M.G1E>'1](@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB
M.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@
M(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N
M9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*
M("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@
M("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@
M(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-
M"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N
M,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B0V]N=&%I;F5R0W!U57-A9V53
M96-O;F1S5&]T86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I
M7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!W:&5R92!,86)E
M;',N8W!U/3U<(G1O=&%L7")<;GP@=VAE<F4@0V]N=&%I;F5R(#T](%PB8V%D
M=FES;W)<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG*$QA8F5L<RYN
M86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B96QS+G!O9"E<
M;GP@97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E;',N8V]N=&%I;F5R
M*5QN?"!W:&5R92!#;VYT86EN97(@/3T@7")<(EQN?"!E>'1E;F0@260@/2!T
M;W-T<FEN9RA,86)E;',N:60I7&Y\('=H97)E($ED(&5N9'-W:71H(%PB+G-L
M:6-E7")<;GP@=VAE<F4@3F%M97-P86-E("$](%PB7")<;GP@:6YV;VME('!R
M;VU?<F%T92@I7&Y\('-U;6UA<FEZ92!686QU93UR;W5N9"AA=F<H5F%L=64I
M*S`N,#`P-2PS*2!B>2!B:6XH5&EM97-T86UP+"`V,#`P,&US*2P@3F%M97-P
M86-E+"!)9%QN?"!S=6UM87)I>F4@5F%L=64]<W5M*%9A;'5E*2!B>2!B:6XH
M5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI+"!.86UE<W!A8V5<;GP@<')O
M:F5C="!4:6UE<W1A;7`L($YA;65S<&%C92P@5F%L=65<;GP@;W)D97(@8GD@
M5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B
M<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@
M("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")5=&EL
M:7IA=&EO;B(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E
M<FEE<R(-"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z(")#
M4%4@57-A9V4@0GD@3F%M97-P86-E(BP-"B`@("`@(")T>7!E(CH@(G1I;65S
M97)I97,B#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-
M"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R
M+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT
M97AT?2(-"B`@("`@('TL#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@
M("`@(")D969A=6QT<R(Z('L-"B`@("`@("`@("`B8V]L;W(B.B![#0H@("`@
M("`@("`@("`B;6]D92(Z(")P86QE='1E+6-L87-S:6,B#0H@("`@("`@("`@
M?2P-"B`@("`@("`@("`B8W5S=&]M(CH@>PT*("`@("`@("`@("`@(F%X:7-"
M;W)D97)3:&]W(CH@9F%L<V4L#0H@("`@("`@("`@("`B87AI<T-E;G1E<F5D
M6F5R;R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X:7-#;VQO<DUO9&4B.B`B
M=&5X="(L#0H@("`@("`@("`@("`B87AI<TQA8F5L(CH@(B(L#0H@("`@("`@
M("`@("`B87AI<U!L86-E;65N="(Z(")A=71O(BP-"B`@("`@("`@("`@(")B
M87)!;&EG;FUE;G0B.B`P+`T*("`@("`@("`@("`@(F1R87=3='EL92(Z(")L
M:6YE(BP-"B`@("`@("`@("`@(")F:6QL3W!A8VET>2(Z(#`L#0H@("`@("`@
M("`@("`B9W)A9&EE;G1-;V1E(CH@(FYO;F4B+`T*("`@("`@("`@("`@(FAI
M9&5&<F]M(CH@>PT*("`@("`@("`@("`@("`B;&5G96YD(CH@9F%L<V4L#0H@
M("`@("`@("`@("`@(")T;V]L=&EP(CH@9F%L<V4L#0H@("`@("`@("`@("`@
M(")V:7HB.B!F86QS90T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")I
M;G-E<G1.=6QL<R(Z(&9A;'-E+`T*("`@("`@("`@("`@(FQI;F5);G1E<G!O
M;&%T:6]N(CH@(FQI;F5A<B(L#0H@("`@("`@("`@("`B;&EN95=I9'1H(CH@
M,2P-"B`@("`@("`@("`@(")P;VEN=%-I>F4B.B`U+`T*("`@("`@("`@("`@
M(G-C86QE1&ES=')I8G5T:6]N(CH@>PT*("`@("`@("`@("`@("`B='EP92(Z
M(")L:6YE87(B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G-H;W=0
M;VEN=',B.B`B;F5V97(B+`T*("`@("`@("`@("`@(G-P86Y.=6QL<R(Z(&9A
M;'-E+`T*("`@("`@("`@("`@(G-T86-K:6YG(CH@>PT*("`@("`@("`@("`@
M("`B9W)O=7`B.B`B02(L#0H@("`@("`@("`@("`@(")M;V1E(CH@(FYO;F4B
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G1H<F5S:&]L9'-3='EL
M92(Z('L-"B`@("`@("`@("`@("`@(FUO9&4B.B`B;V9F(@T*("`@("`@("`@
M("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(FQI;FMS(CH@6PT*("`@
M("`@("`@("`@>PT*("`@("`@("`@("`@("`B=&ET;&4B.B`B5FEE=R!0;V1S
M(BP-"B`@("`@("`@("`@("`@(G5R;"(Z(")H='1P<SHO+V=R869A;F$M;&%R
M9V5C;'5S=&5R+60T9F9G=F1N9G1B=F-M9G,N8V-A+F=R869A;F$N87IU<F4N
M8V]M+V0O8V1U>'`Y;F)M:C%F:V(O<&]D<S]O<F=)9#TQ)G9A<BU.86UE<W!A
M8V4])'M?7V9I96QD+FQA8F5L<RY.86UE<W!A8V5](@T*("`@("`@("`@("`@
M?2P-"B`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@(G1I=&QE(CH@(E9I
M97<@3F%M97-P86-E(BP-"B`@("`@("`@("`@("`@(G5R;"(Z(")H='1P<SHO
M+V=R869A;F$M;&%R9V5C;'5S=&5R+60T9F9G=F1N9G1B=F-M9G,N8V-A+F=R
M869A;F$N87IU<F4N8V]M+V0O9F1V9FIZ>#EL;V-U.&$O;F%M97-P86-E<S]O
M<F=)9#TQ)B1[7U]U<FQ?=&EM95]R86YG97TF=F%R+4YA;65S<&%C93TD>U]?
M9FEE;&0N;&%B96QS+DYA;65S<&%C97TF)'M#;'5S=&5R.G%U97)Y<&%R86U]
M(@T*("`@("`@("`@("`@?0T*("`@("`@("`@(%TL#0H@("`@("`@("`@(FUA
M<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@("`@
M("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B<W1E
M<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L
M;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(&YU;&P-
M"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@
M("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@("`@("`@(")V86QU
M92(Z(#@P#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@
M("`@("!]+`T*("`@("`@("`@(")U;FET(CH@(F)Y=&5S(@T*("`@("`@("!]
M+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6UT-"B`@("`@('TL#0H@("`@("`B
M9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`X+`T*("`@("`@("`B=R(Z(#$R
M+`T*("`@("`@("`B>"(Z(#$R+`T*("`@("`@("`B>2(Z(#$Y#0H@("`@("!]
M+`T*("`@("`@(FED(CH@.2P-"B`@("`@(")I;G1E<G9A;"(Z("(Q;2(L#0H@
M("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(FQE9V5N9"(Z('L-"B`@("`@
M("`@("`B8V%L8W,B.B!;72P-"B`@("`@("`@("`B9&ES<&QA>4UO9&4B.B`B
M;&ES="(L#0H@("`@("`@("`@(G!L86-E;65N="(Z(")B;W1T;VTB+`T*("`@
M("`@("`@(")S:&]W3&5G96YD(CH@=')U90T*("`@("`@("!]+`T*("`@("`@
M("`B=&]O;'1I<"(Z('L-"B`@("`@("`@("`B;6]D92(Z(")S:6YG;&4B+`T*
M("`@("`@("`@(")S;W)T(CH@(FYO;F4B#0H@("`@("`@('T-"B`@("`@('TL
M#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@("`@(")/
M<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC
M<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B
M='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C
M92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-
M"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@
M("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R97-S
M:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@
M("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@
M("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E
M(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B
M.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@
M("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@
M("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@
M("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<DUE;6]R>5=O<FMI;F=3971"
M>71E<UQN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@=VAE
M<F4@3&%B96QS("%H87,@7")I9%PB7&Y\('=H97)E($-L=7-T97(]/5PB)$-L
M=7-T97)<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG*$QA8F5L<RYN
M86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B96QS+G!O9"E<
M;GP@97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E;',N8V]N=&%I;F5R
M*5QN?"!W:&5R92!#;VYT86EN97(@(3T@7")<(EQN?"!E>'1E;F0@5F%L=64]
M5F%L=65<;GP@<W5M;6%R:7IE(%9A;'5E/6%V9RA686QU92D@8GD@8FEN*%1I
M;65S=&%M<"P@)%]?=&EM94EN=&5R=F%L*2P@3F%M97-P86-E+"!0;V0L($-O
M;G1A:6YE<EQN?"!S=6UM87)I>F4@5F%L=64]<W5M*%9A;'5E*2!B>2!B:6XH
M5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI+"!.86UE<W!A8V4L(%!O9%QN
M?"!S=6UM87)I>F4@5F%L=64]<W5M*%9A;'5E*2!B>2!B:6XH5&EM97-T86UP
M+"`D7U]T:6UE26YT97)V86PI+"!.86UE<W!A8V5<;GP@<')O:F5C="!4:6UE
M<W1A;7`L($YA;65S<&%C92P@5F%L=65<;GP@;W)D97(@8GD@5&EM97-T86UP
M(&%S8UQN7&Y<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L
M#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R
M87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B571I;&EZ871I
M;VXB+`T*("`@("`@("`@(")R97-U;'1&;W)M870B.B`B=&EM95]S97)I97,B
M#0H@("`@("`@('T-"B`@("`@(%TL#0H@("`@("`B=&ET;&4B.B`B365M;W)Y
M(%5S86=E($)Y($YA;65S<&%C92(L#0H@("`@("`B='EP92(Z(")T:6UE<V5R
M:65S(@T*("`@('T-"B`@72P-"B`@(G)E9G)E<V@B.B`B,6TB+`T*("`B<V-H
M96UA5F5R<VEO;B(Z(#,Y+`T*("`B=&%G<R(Z(%M=+`T*("`B=&5M<&QA=&EN
M9R(Z('L-"B`@("`B;&ES="(Z(%L-"B`@("`@('L-"B`@("`@("`@(FAI9&4B
M.B`P+`T*("`@("`@("`B:6YC;'5D94%L;"(Z(&9A;'-E+`T*("`@("`@("`B
M;75L=&DB.B!F86QS92P-"B`@("`@("`@(FYA;64B.B`B1&%T87-O=7)C92(L
M#0H@("`@("`@(")O<'1I;VYS(CH@6UTL#0H@("`@("`@(")Q=65R>2(Z(")G
M<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@
M("`@(")Q=65R>59A;'5E(CH@(B(L#0H@("`@("`@(")R969R97-H(CH@,2P-
M"B`@("`@("`@(G)E9V5X(CH@(B(L#0H@("`@("`@(")S:VEP57)L4WEN8R(Z
M(&9A;'-E+`T*("`@("`@("`B='EP92(Z(")D871A<V]U<F-E(@T*("`@("`@
M?2P-"B`@("`@('L-"B`@("`@("`@(F-U<G)E;G0B.B![#0H@("`@("`@("`@
M(G-E;&5C=&5D(CH@9F%L<V4L#0H@("`@("`@("`@(G1E>'0B.B`B(BP-"B`@
M("`@("`@("`B=F%L=64B.B`B(@T*("`@("`@("!]+`T*("`@("`@("`B9&%T
M87-O=7)C92(Z('L-"B`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E
M+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@('TL#0H@("`@("`@(")D
M969I;FET:6]N(CH@(D-O;G1A:6YE<D-P=55S86=E4V5C;VYD<U1O=&%L('P@
M=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I('P@9&ES=&EN8W0@0VQU
M<W1E<B(L#0H@("`@("`@(")H:61E(CH@,"P-"B`@("`@("`@(FEN8VQU9&5!
M;&PB.B!F86QS92P-"B`@("`@("`@(FUU;'1I(CH@9F%L<V4L#0H@("`@("`@
M(")N86UE(CH@(D-L=7-T97(B+`T*("`@("`@("`B;W!T:6]N<R(Z(%M=+`T*
M("`@("`@("`B<75E<GDB.B![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E
M+`T*("`@("`@("`@(")C;'5S=&5R57)I(CH@(B(L#0H@("`@("`@("`@(F1A
M=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@
M>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E
M>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*
M("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@
M("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N
M-2(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<D-P=55S86=E4V5C
M;VYD<U1O=&%L('P@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I('P@
M9&ES=&EN8W0@0VQU<W1E<B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@
M(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@
M("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T
M(CH@(G1A8FQE(@T*("`@("`@("!]+`T*("`@("`@("`B<F5F<F5S:"(Z(#$L
M#0H@("`@("`@(")R96=E>"(Z("(B+`T*("`@("`@("`B<VMI<%5R;%-Y;F,B
M.B!F86QS92P-"B`@("`@("`@(G-O<G0B.B`P+`T*("`@("`@("`B='EP92(Z
M(")Q=65R>2(-"B`@("`@('T-"B`@("!=#0H@('TL#0H@(")T:6UE(CH@>PT*
M("`@(")F<F]M(CH@(FYO=RTS,&TB+`T*("`@(")T;R(Z(")N;W<B#0H@('TL
M#0H@(")T:6UE<&EC:V5R(CH@>PT*("`@(")N;W=$96QA>2(Z("(Q;2(-"B`@
M?2P-"B`@(G1I;65Z;VYE(CH@(F)R;W=S97(B+`T*("`B=&ET;&4B.B`B0VQU
A<W1E<B!);F9O(BP-"B`@(G=E96M3=&%R="(Z("(B#0I]
`
end
SHAR_EOF
  (set 20 24 12 06 20 59 58 'dashboards/cluster-info.json'
   eval "${shar_touch}") && \
  chmod 0644 'dashboards/cluster-info.json'
if test $? -ne 0
then ${echo} "restore of dashboards/cluster-info.json failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'dashboards/cluster-info.json': 'MD5 check failed'
       ) << \SHAR_EOF
4db2dd70a6fe3cae666406c08ede6906  dashboards/cluster-info.json
SHAR_EOF

else
test `LC_ALL=C wc -c < 'dashboards/cluster-info.json'` -ne 53133 && \
  ${echo} "restoration warning:  size of 'dashboards/cluster-info.json' is not 53133"
  fi
# ============= dashboards/pods.json ==============
  sed 's/^X//' << 'SHAR_EOF' | uudecode &&
begin 600 dashboards/pods.json
M>PT*("`B86YN;W1A=&EO;G,B.B![#0H@("`@(FQI<W0B.B!;#0H@("`@("![
M#0H@("`@("`@(")B=6EL=$EN(CH@,2P-"B`@("`@("`@(F1A=&%S;W5R8V4B
M.B![#0H@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X
M<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@(")U:60B.B`B)'M$871A
M<V]U<F-E.G1E>'1](@T*("`@("`@("!]+`T*("`@("`@("`B96YA8FQE(CH@
M=')U92P-"B`@("`@("`@(FAI9&4B.B!T<G5E+`T*("`@("`@("`B:6-O;D-O
M;&]R(CH@(G)G8F$H,"P@,C$Q+"`R-34L(#$I(BP-"B`@("`@("`@(FYA;64B
M.B`B06YN;W1A=&EO;G,@)B!!;&5R=',B+`T*("`@("`@("`B='EP92(Z(")D
M87-H8F]A<F0B#0H@("`@("!]#0H@("`@70T*("!]+`T*("`B961I=&%B;&4B
M.B!T<G5E+`T*("`B9FES8V%L665A<E-T87)T36]N=&@B.B`P+`T*("`B9W)A
M<&A4;V]L=&EP(CH@,"P-"B`@(FED(CH@-3$L#0H@(")L:6YK<R(Z(%L-"B`@
M("![#0H@("`@("`B87-$<F]P9&]W;B(Z(&9A;'-E+`T*("`@("`@(FEC;VXB
M.B`B97AT97)N86P@;&EN:R(L#0H@("`@("`B:6YC;'5D959A<G,B.B!T<G5E
M+`T*("`@("`@(FME97!4:6UE(CH@=')U92P-"B`@("`@(")T86=S(CH@6UTL
M#0H@("`@("`B=&%R9V5T0FQA;FLB.B!F86QS92P-"B`@("`@(")T:71L92(Z
M(")#;'5S=&5R(BP-"B`@("`@(")T;V]L=&EP(CH@(B(L#0H@("`@("`B='EP
M92(Z(")L:6YK(BP-"B`@("`@(")U<FPB.B`B:'1T<',Z+R]G<F%F86YA+6QA
M<F=E8VQU<W1E<BUD-&9F9W9D;F9T8G9C;69S+F-C82YG<F%F86YA+F%Z=7)E
M+F-O;2]D+V5D=C%L8FQE9W5U=W=B+V-L=7-T97(M:6YF;S]O<F=)9#TQ(@T*
M("`@('T-"B`@72P-"B`@(G!A;F5L<R(Z(%L-"B`@("![#0H@("`@("`B9&%T
M87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD
M871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[
M1&%T87-O=7)C93IT97AT?2(-"B`@("`@('TL#0H@("`@("`B9&5S8W)I<'1I
M;VXB.B`B(BP-"B`@("`@(")F:65L9$-O;F9I9R(Z('L-"B`@("`@("`@(F1E
M9F%U;'1S(CH@>PT*("`@("`@("`@(")C;VQO<B(Z('L-"B`@("`@("`@("`@
M(")M;V1E(CH@(G1H<F5S:&]L9',B#0H@("`@("`@("`@?2P-"B`@("`@("`@
M("`B8W5S=&]M(CH@>PT*("`@("`@("`@("`@(F%L:6=N(CH@(F%U=&\B+`T*
M("`@("`@("`@("`@(F-E;&Q/<'1I;VYS(CH@>PT*("`@("`@("`@("`@("`B
M='EP92(Z(")A=71O(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")F
M:6QT97)A8FQE(CH@=')U92P-"B`@("`@("`@("`@(")I;G-P96-T(CH@9F%L
M<V4-"B`@("`@("`@("!]+`T*("`@("`@("`@(")F:65L9$UI;DUA>"(Z(&9A
M;'-E+`T*("`@("`@("`@(")M87!P:6YG<R(Z(%M=+`T*("`@("`@("`@(")T
M:')E<VAO;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B86)S;VQU=&4B
M+`T*("`@("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@("`@("![#0H@
M("`@("`@("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@("`@
M("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@('TL#0H@("`@("`@
M("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B<F5D(BP-"B`@
M("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*("`@("`@("`@("`@("!]#0H@
M("`@("`@("`@("!=#0H@("`@("`@("`@?0T*("`@("`@("!]+`T*("`@("`@
M("`B;W9E<G)I9&5S(CH@6PT*("`@("`@("`@('L-"B`@("`@("`@("`@(")M
M871C:&5R(CH@>PT*("`@("`@("`@("`@("`B:60B.B`B8GE.86UE(BP-"B`@
M("`@("`@("`@("`@(F]P=&EO;G,B.B`B0U!5(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")P<F]P97)T:65S(CH@6PT*("`@("`@("`@("`@("![
M#0H@("`@("`@("`@("`@("`@(FED(CH@(F1E8VEM86QS(BP-"B`@("`@("`@
M("`@("`@("`B=F%L=64B.B`S#0H@("`@("`@("`@("`@('T-"B`@("`@("`@
M("`@(%T-"B`@("`@("`@("!]+`T*("`@("`@("`@('L-"B`@("`@("`@("`@
M(")M871C:&5R(CH@>PT*("`@("`@("`@("`@("`B:60B.B`B8GE.86UE(BP-
M"B`@("`@("`@("`@("`@(F]P=&EO;G,B.B`B365M(@T*("`@("`@("`@("`@
M?2P-"B`@("`@("`@("`@(")P<F]P97)T:65S(CH@6PT*("`@("`@("`@("`@
M("![#0H@("`@("`@("`@("`@("`@(FED(CH@(G5N:70B+`T*("`@("`@("`@
M("`@("`@(")V86QU92(Z(")D96-B>71E<R(-"B`@("`@("`@("`@("`@?0T*
M("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@("`@("`@("`@>PT*("`@
M("`@("`@("`@(FUA=&-H97(B.B![#0H@("`@("`@("`@("`@(")I9"(Z(")B
M>4YA;64B+`T*("`@("`@("`@("`@("`B;W!T:6]N<R(Z(")296%D>2(-"B`@
M("`@("`@("`@('TL#0H@("`@("`@("`@("`B<')O<&5R=&EE<R(Z(%L-"B`@
M("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")I9"(Z(")M87!P:6YG
M<R(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@6PT*("`@("`@("`@("`@
M("`@("`@>PT*("`@("`@("`@("`@("`@("`@("`B;W!T:6]N<R(Z('L-"B`@
M("`@("`@("`@("`@("`@("`@("`B,"(Z('L-"B`@("`@("`@("`@("`@("`@
M("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@("`@("`@("`@("`@
M("`@(FEN9&5X(CH@,0T*("`@("`@("`@("`@("`@("`@("`@('TL#0H@("`@
M("`@("`@("`@("`@("`@("`@(C$B.B![#0H@("`@("`@("`@("`@("`@("`@
M("`@("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@("`@("`@
M("`@(FEN9&5X(CH@,`T*("`@("`@("`@("`@("`@("`@("`@('T-"B`@("`@
M("`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@("`@("`@(G1Y<&4B
M.B`B=F%L=64B#0H@("`@("`@("`@("`@("`@("!]#0H@("`@("`@("`@("`@
M("`@70T*("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@("![#0H@("`@
M("`@("`@("`@("`@(FED(CH@(F-U<W1O;2YC96QL3W!T:6]N<R(L#0H@("`@
M("`@("`@("`@("`@(G9A;'5E(CH@>PT*("`@("`@("`@("`@("`@("`@(FUO
M9&4B.B`B8F%S:6,B+`T*("`@("`@("`@("`@("`@("`@(G1Y<&4B.B`B8V]L
M;W(M8F%C:V=R;W5N9"(-"B`@("`@("`@("`@("`@("!]#0H@("`@("`@("`@
M("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@("`@("!]+`T*("`@("`@("`@
M('L-"B`@("`@("`@("`@(")M871C:&5R(CH@>PT*("`@("`@("`@("`@("`B
M:60B.B`B8GE.86UE(BP-"B`@("`@("`@("`@("`@(F]P=&EO;G,B.B`B4F5A
M9'DB#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G!R;W!E<G1I97,B
M.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B:60B.B`B
M8W5S=&]M+G=I9'1H(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*
M("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@("![#0H@("`@("`@("`@
M("`@("`@(FED(CH@(F-U<W1O;2YC96QL3W!T:6]N<R(L#0H@("`@("`@("`@
M("`@("`@(G9A;'5E(CH@>PT*("`@("`@("`@("`@("`@("`@(G1Y<&4B.B`B
M875T;R(-"B`@("`@("`@("`@("`@("!]#0H@("`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B:60B.B`B8W5S=&]M
M+F%L:6=N(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`B8V5N=&5R(@T*
M("`@("`@("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?0T*
M("`@("`@("!=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@
M("`@(")H(CH@,3(L#0H@("`@("`@(")W(CH@,34L#0H@("`@("`@(")X(CH@
M,"P-"B`@("`@("`@(GDB.B`P#0H@("`@("!]+`T*("`@("`@(FED(CH@,34L
M#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(F-E;&Q(96EG:'0B.B`B
M<VTB+`T*("`@("`@("`B9F]O=&5R(CH@>PT*("`@("`@("`@(")C;W5N=%)O
M=W,B.B!F86QS92P-"B`@("`@("`@("`B96YA8FQE4&%G:6YA=&EO;B(Z('1R
M=64L#0H@("`@("`@("`@(F9I96QD<R(Z("(B+`T*("`@("`@("`@(")R961U
M8V5R(CH@6PT*("`@("`@("`@("`@(G-U;2(-"B`@("`@("`@("!=+`T*("`@
M("`@("`@(")S:&]W(CH@=')U90T*("`@("`@("!]+`T*("`@("`@("`B<VAO
M=TAE861E<B(Z('1R=64L#0H@("`@("`@(")S;W)T0GDB.B!;70T*("`@("`@
M?2P-"B`@("`@(")P;'5G:6Y697)S:6]N(CH@(C$P+C0N,3$B+`T*("`@("`@
M(G1A<F=E=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@
M9F%L<V4L#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@
M("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B
M9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@
M("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@
M("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@
M(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;
M72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@
M?2P-"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E
M>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@
M("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@
M(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@
M(")Q=65R>2(Z(")<;FQE="!P;V13=&%T=7,]*%QN("`@($MU8F50;V13=&%T
M=7-296%D>5QN("`@('P@=VAE<F4@5&EM97-T86UP(#X](&%G;R@S,&TI7&X@
M("`@?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;B`@("!\(&5X=&5N
M9"!.86UE<W!A8V4]=&]S=')I;F<H3&%B96QS+FYA;65S<&%C92E<;B`@("!\
M(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B96QS+G!O9"E<;B`@("!\(&5X=&5N
M9"!#;VYT86EN97(]=&]S=')I;F<H3&%B96QS+F-O;G1A:6YE<BE<;B`@("!\
M('=H97)E($YA;65S<&%C92`]/2!<(B1.86UE<W!A8V5<(EQN("`@('P@=VAE
M<F4@3&%B96QS+F-O;F1I=&EO;B`]/2!<(G1R=65<(EQN("`@('P@<W5M;6%R
M:7IE(&%R9U]M87@H5&EM97-T86UP+"!686QU92D@8GD@3F%M97-P86-E+"!0
M;V1<;B`@("!\('!R;VIE8W0M87=A>2!4:6UE<W1A;7!<;B`@("!\('!R;VIE
M8W0M<F5N86UE(%)E861Y/59A;'5E7&XI.UQN;&5T(&-P=55S86=E/2A<;B`@
M("!#;VYT86EN97)#<'55<V%G95-E8V]N9'-4;W1A;%QN("`@('P@=VAE<F4@
M5&EM97-T86UP(#X](&%G;R@S,&TI7&X@("`@?"!W:&5R92!,86)E;',N8W!U
M(#T](%PB=&]T86Q<(EQN("`@('P@=VAE<F4@0V]N=&%I;F5R(#T](%PB8V%D
M=FES;W)<(EQN("`@('P@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&X@
M("`@?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG*$QA8F5L<RYN86UE<W!A
M8V4I7&X@("`@?"!E>'1E;F0@4&]D/71O<W1R:6YG*$QA8F5L<RYP;V0I7&X@
M("`@?"!E>'1E;F0@0V]N=&%I;F5R/71O<W1R:6YG*$QA8F5L<RYC;VYT86EN
M97(I7&X@("`@?"!W:&5R92!.86UE<W!A8V4@/3T@7"(D3F%M97-P86-E7")<
M;B`@("!\('=H97)E($-O;G1A:6YE<B`]/2!<(EPB7&X@("`@?"!W:&5R92!,
M86)E;',N:60@96YD<W=I=&@@7"(N<VQI8V5<(EQN("`@('P@:6YV;VME('!R
M;VU?<F%T92@I7&X@("`@?"!S=6UM87)I>F4@0U!5/7)O=6YD*&%V9RA686QU
M92DK,"XP,#`U+#,I(&)Y($YA;65S<&%C92P@4&]D+"!T;W-T<FEN9RA,86)E
M;',N:60I7&X@("`@?"!P<F]J96-T+6%W87D@3&%B96QS7VED7&XI.UQN;&5T
M(&UE;55S86=E/2A<;B`@("!#;VYT86EN97)-96UO<GE5<V%G94)Y=&5S7&X@
M("`@?"!W:&5R92!4:6UE<W1A;7`@/CT@86=O*#,P;2E<;B`@("!\('=H97)E
M($-L=7-T97(]/5PB)$-L=7-T97)<(EQN("`@('P@97AT96YD($YA;65S<&%C
M93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN("`@('P@97AT96YD(%!O
M9#UT;W-T<FEN9RA,86)E;',N<&]D*5QN("`@('P@97AT96YD($-O;G1A:6YE
M<CUT;W-T<FEN9RA,86)E;',N8V]N=&%I;F5R*5QN("`@('P@=VAE<F4@3F%M
M97-P86-E(#T](%PB)$YA;65S<&%C95PB7&X@("`@?"!W:&5R92!#;VYT86EN
M97(@/3T@7")<(EQN("`@('P@=VAE<F4@3&%B96QS+FED(&5N9'-W:71H(%PB
M+G-L:6-E7")<;B`@("!\('-U;6UA<FEZ92!-96T]<F]U;F0H879G*%9A;'5E
M*2LP+C`P,#4L,RD@8GD@3F%M97-P86-E+"!0;V0L('1O<W1R:6YG*$QA8F5L
M<RYI9"E<;B`@("!\('!R;VIE8W0M87=A>2!,86)E;'-?:61<;BD[7&YP;V13
M=&%T=7-<;B`@("!\(&IO:6X@:VEN9#UL969T;W5T97(@8W!U57-A9V4@;VX@
M3F%M97-P86-E+"!0;V0@?"!P<F]J96-T+6%W87D@4&]D,2P@3F%M97-P86-E
M,5QN("`@('P@:F]I;B!K:6YD/6QE9G1O=71E<B!M96U5<V%G92!O;B!.86UE
M<W!A8V4L(%!O9"!\('!R;VIE8W0M87=A>2!0;V0Q+"!.86UE<W!A8V4Q7&X@
M("`@?"!E>'1E;F0@4F5A9'D]8V%S92A296%D>2`]/2`Q+"!<(N*<A5PB+"!<
M(N*=C%PB*5QN("`@(%QN7&XB+`T*("`@("`@("`@(")Q=65R>5-O=7)C92(Z
M(")R87<B+`T*("`@("`@("`@(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@("`@
M("`@("`B<F%W36]D92(Z('1R=64L#0H@("`@("`@("`@(G)E9DED(CH@(D$B
M+`T*("`@("`@("`@(")R97-U;'1&;W)M870B.B`B=&%B;&4B#0H@("`@("`@
M('T-"B`@("`@(%TL#0H@("`@("`B='EP92(Z(")T86)L92(-"B`@("!]+`T*
M("`@('L-"B`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`B='EP92(Z
M(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@
M("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@?2P-
M"B`@("`@(")D97-C<FEP=&EO;B(Z("(B+`T*("`@("`@(F9I96QD0V]N9FEG
M(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R
M(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B=&AR97-H;VQD<R(-"B`@("`@
M("`@("!]+`T*("`@("`@("`@(")C=7-T;VTB.B![#0H@("`@("`@("`@("`B
M86QI9VXB.B`B875T;R(L#0H@("`@("`@("`@("`B8V5L;$]P=&EO;G,B.B![
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%U=&\B#0H@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@(F9I;'1E<F%B;&4B.B!F86QS92P-"B`@("`@("`@
M("`@(")I;G-P96-T(CH@9F%L<V4-"B`@("`@("`@("!]+`T*("`@("`@("`@
M(")F:65L9$UI;DUA>"(Z(&9A;'-E+`T*("`@("`@("`@(")M87!P:6YG<R(Z
M(%M=+`T*("`@("`@("`@(")T:')E<VAO;&1S(CH@>PT*("`@("`@("`@("`@
M(FUO9&4B.B`B86)S;VQU=&4B+`T*("`@("`@("`@("`@(G-T97!S(CH@6PT*
M("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(F=R
M965N(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B
M8V]L;W(B.B`B<F5D(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*
M("`@("`@("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?0T*
M("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6PT*("`@("`@("`@
M('L-"B`@("`@("`@("`@(")M871C:&5R(CH@>PT*("`@("`@("`@("`@("`B
M:60B.B`B8GE.86UE(BP-"B`@("`@("`@("`@("`@(F]P=&EO;G,B.B`B1FEE
M;&0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G!R;W!E<G1I97,B
M.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B:60B.B`B
M8W5S=&]M+G=I9'1H(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`Q,C`-
M"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@
M("`@("`@(")I9"(Z(")C=7-T;VTN8V5L;$]P=&EO;G,B+`T*("`@("`@("`@
M("`@("`@(")V86QU92(Z('L-"B`@("`@("`@("`@("`@("`@(")T>7!E(CH@
M(F%U=&\B#0H@("`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@("!]+`T*
M("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(FED(CH@(F-U<W1O
M;2YA;&EG;B(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@(FQE9G0B#0H@
M("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@("`@("!]+`T*
M("`@("`@("`@('L-"B`@("`@("`@("`@(")M871C:&5R(CH@>PT*("`@("`@
M("`@("`@("`B:60B.B`B8GE.86UE(BP-"B`@("`@("`@("`@("`@(F]P=&EO
M;G,B.B`B5F%L=64B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G!R
M;W!E<G1I97,B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@
M("`B:60B.B`B8W5S=&]M+F-E;&Q/<'1I;VYS(BP-"B`@("`@("`@("`@("`@
M("`B=F%L=64B.B![#0H@("`@("`@("`@("`@("`@("`B='EP92(Z(")A=71O
M(@T*("`@("`@("`@("`@("`@('T-"B`@("`@("`@("`@("`@?0T*("`@("`@
M("`@("`@70T*("`@("`@("`@('T-"B`@("`@("`@70T*("`@("`@?2P-"B`@
M("`@(")G<FED4&]S(CH@>PT*("`@("`@("`B:"(Z(#$R+`T*("`@("`@("`B
M=R(Z(#0L#0H@("`@("`@(")X(CH@,34L#0H@("`@("`@(")Y(CH@,`T*("`@
M("`@?2P-"B`@("`@(")I9"(Z(#$V+`T*("`@("`@(F]P=&EO;G,B.B![#0H@
M("`@("`@(")C96QL2&5I9VAT(CH@(G-M(BP-"B`@("`@("`@(F9O;W1E<B(Z
M('L-"B`@("`@("`@("`B8V]U;G12;W=S(CH@9F%L<V4L#0H@("`@("`@("`@
M(F5N86)L95!A9VEN871I;VXB.B!F86QS92P-"B`@("`@("`@("`B9FEE;&1S
M(CH@(B(L#0H@("`@("`@("`@(G)E9'5C97(B.B!;#0H@("`@("`@("`@("`B
M<W5M(@T*("`@("`@("`@(%TL#0H@("`@("`@("`@(G-H;W<B.B!F86QS90T*
M("`@("`@("!]+`T*("`@("`@("`B<VAO=TAE861E<B(Z(&9A;'-E+`T*("`@
M("`@("`B<V]R=$)Y(CH@6UT-"B`@("`@('TL#0H@("`@("`B<&QU9VEN5F5R
M<VEO;B(Z("(Q,"XT+C$Q(BP-"B`@("`@(")T87)G971S(CH@6PT*("`@("`@
M("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D
M871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z
M('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE
M>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$
M871A<V]U<F-E.G1E>'1](@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X
M<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@
M("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T
M>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D
M=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@
M("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I
M;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@
M("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G:6Y697)S
M:6]N(CH@(C4N,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B7&YL970@<&]D
M4W1A='5S/2A<;B`@("!+=6)E4&]D4W1A='5S4F5A9'E<;B`@("!\('=H97)E
M(%1I;65S=&%M<"`^/2!A9V\H,S!M*5QN("`@('P@=VAE<F4@0VQU<W1E<CT]
M7"(D0VQU<W1E<EPB7&X@("`@?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG
M*$QA8F5L<RYN86UE<W!A8V4I7&X@("`@?"!E>'1E;F0@4&]D/71O<W1R:6YG
M*$QA8F5L<RYP;V0I7&X@("`@?"!E>'1E;F0@0V]N=&%I;F5R/71O<W1R:6YG
M*$QA8F5L<RYC;VYT86EN97(I7&X@("`@?"!W:&5R92!.86UE<W!A8V4@/3T@
M7"(D3F%M97-P86-E7")<;B`@("!\('=H97)E($QA8F5L<RYC;VYD:71I;VX@
M/3T@7")T<G5E7")<;B`@("!\('-U;6UA<FEZ92!A<F=?;6%X*%1I;65S=&%M
M<"P@5F%L=64I(&)Y($YA;65S<&%C92P@4&]D7&X@("`@?"!P<F]J96-T+6%W
M87D@5&EM97-T86UP7&X@("`@?"!P<F]J96-T+7)E;F%M92!296%D>3U686QU
M95QN*3M<;FQE="!C<'55<V%G93TH7&X@("`@0V]N=&%I;F5R0W!U57-A9V53
M96-O;F1S5&]T86Q<;B`@("!\('=H97)E(%1I;65S=&%M<"`^/2!A9V\H,S!M
M*5QN("`@('P@=VAE<F4@3&%B96QS+F-P=2`]/2!<(G1O=&%L7")<;B`@("!\
M('=H97)E($-O;G1A:6YE<B`]/2!<(F-A9'9I<V]R7")<;B`@("!\('=H97)E
M($-L=7-T97(]/5PB)$-L=7-T97)<(EQN("`@('P@97AT96YD($YA;65S<&%C
M93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN("`@('P@97AT96YD(%!O
M9#UT;W-T<FEN9RA,86)E;',N<&]D*5QN("`@('P@97AT96YD($-O;G1A:6YE
M<CUT;W-T<FEN9RA,86)E;',N8V]N=&%I;F5R*5QN("`@('P@=VAE<F4@3F%M
M97-P86-E(#T](%PB)$YA;65S<&%C95PB7&X@("`@?"!W:&5R92!#;VYT86EN
M97(@/3T@7")<(EQN("`@('P@=VAE<F4@3&%B96QS+FED(&5N9'-W:71H(%PB
M+G-L:6-E7")<;B`@("!\(&EN=F]K92!P<F]M7W)A=&4H*5QN("`@('P@<W5M
M;6%R:7IE($-053UR;W5N9"AA=F<H5F%L=64I*S`N,#`P-2PS*2!B>2!.86UE
M<W!A8V4L(%!O9"P@=&]S=')I;F<H3&%B96QS+FED*5QN("`@('P@<')O:F5C
M="UA=V%Y($QA8F5L<U]I9%QN*3M<;FQE="!M96U5<V%G93TH7&X@("`@0V]N
M=&%I;F5R365M;W)Y57-A9V5">71E<UQN("`@('P@=VAE<F4@5&EM97-T86UP
M(#X](&%G;R@S,&TI7&X@("`@?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R
M7")<;B`@("!\(&5X=&5N9"!.86UE<W!A8V4]=&]S=')I;F<H3&%B96QS+FYA
M;65S<&%C92E<;B`@("!\(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B96QS+G!O
M9"E<;B`@("!\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H3&%B96QS+F-O
M;G1A:6YE<BE<;B`@("!\('=H97)E($YA;65S<&%C92`]/2!<(B1.86UE<W!A
M8V5<(EQN("`@('P@=VAE<F4@0V]N=&%I;F5R(#T](%PB7")<;B`@("!\('=H
M97)E($QA8F5L<RYI9"!E;F1S=VET:"!<(BYS;&EC95PB7&X@("`@?"!S=6UM
M87)I>F4@365M/7)O=6YD*&%V9RA686QU92DK,"XP,#`U+#,I(&)Y($YA;65S
M<&%C92P@4&]D+"!T;W-T<FEN9RA,86)E;',N:60I7&X@("`@?"!P<F]J96-T
M+6%W87D@3&%B96QS7VED7&XI.UQN<&]D4W1A='5S7&X@("`@?"!J;VEN(&MI
M;F0];&5F=&]U=&5R(&-P=55S86=E(&]N($YA;65S<&%C92P@4&]D('P@<')O
M:F5C="UA=V%Y(%!O9#$L($YA;65S<&%C93%<;B`@("!\(&IO:6X@:VEN9#UL
M969T;W5T97(@;65M57-A9V4@;VX@3F%M97-P86-E+"!0;V0@?"!P<F]J96-T
M+6%W87D@4&]D,2P@3F%M97-P86-E,5QN("`@('P@97AT96YD(%)E861Y/6-A
M<V4H4F5A9'D@/3T@,2P@7"+BG(5<(BP@7"+BG8Q<(BE<;B`@("!\('=H97)E
M($YA;65S<&%C92`]/2!<(B1.86UE<W!A8V5<(EQN("`@('P@=VAE<F4@4&]D
M(#T](%PB)%!O9%PB7&X@("`@?"!L:6UI="`Q7&X@("`@?"!E>'1E;F0@4F]W
M/7!A8VM?86QL*"D@7&X@("`@?"!M=BUE>'!A;F0@4F]W7&X@("`@?"!E>'1E
M;F0@1FEE;&0]8F%G7VME>7,H4F]W*5LP75QN("`@('P@97AT96YD(%9A;'5E
M/5)O=UMT;W-T<FEN9RA&:65L9"E=7&X@("`@?"!P<F]J96-T('1O<W1R:6YG
M*$9I96QD*2P@=&]S=')I;F<H5F%L=64I7&X@("`@7&Y<;B(L#0H@("`@("`@
M("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP
M92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@
M("`@("`B<F5F260B.B`B02(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z
M(")T86)L92(-"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T>7!E(CH@
M(G1A8FQE(@T*("`@('TL#0H@("`@>PT*("`@("`@(F-O;&QA<'-E9"(Z(&9A
M;'-E+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@,2P-"B`@
M("`@("`@(G<B.B`R-"P-"B`@("`@("`@(G@B.B`P+`T*("`@("`@("`B>2(Z
M(#$R#0H@("`@("!]+`T*("`@("`@(FED(CH@-RP-"B`@("`@(")P86YE;',B
M.B!;72P-"B`@("`@(")T:71L92(Z(")#4%4B+`T*("`@("`@(G1Y<&4B.B`B
M<F]W(@T*("`@('TL#0H@("`@>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@
M("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD
M871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X
M='TB#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@
M("`B9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@
M("`@("`@(FUO9&4B.B`B<&%L971T92UC;&%S<VEC(@T*("`@("`@("`@('TL
M#0H@("`@("`@("`@(F-U<W1O;2(Z('L-"B`@("`@("`@("`@(")A>&ES0F]R
M9&5R4VAO=R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X:7-#96YT97)E9%IE
M<F\B.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES0V]L;W)-;V1E(CH@(G1E
M>'0B+`T*("`@("`@("`@("`@(F%X:7-,86)E;"(Z("(B+`T*("`@("`@("`@
M("`@(F%X:7-0;&%C96UE;G0B.B`B875T;R(L#0H@("`@("`@("`@("`B87AI
M<U-O9G1-:6XB.B`P+`T*("`@("`@("`@("`@(F)A<D%L:6=N;65N="(Z(#`L
M#0H@("`@("`@("`@("`B9')A=U-T>6QE(CH@(FQI;F4B+`T*("`@("`@("`@
M("`@(F9I;&Q/<&%C:71Y(CH@,3`L#0H@("`@("`@("`@("`B9W)A9&EE;G1-
M;V1E(CH@(FYO;F4B+`T*("`@("`@("`@("`@(FAI9&5&<F]M(CH@>PT*("`@
M("`@("`@("`@("`B;&5G96YD(CH@9F%L<V4L#0H@("`@("`@("`@("`@(")T
M;V]L=&EP(CH@9F%L<V4L#0H@("`@("`@("`@("`@(")V:7HB.B!F86QS90T*
M("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")I;G-E<G1.=6QL<R(Z(&9A
M;'-E+`T*("`@("`@("`@("`@(FQI;F5);G1E<G!O;&%T:6]N(CH@(G-M;V]T
M:"(L#0H@("`@("`@("`@("`B;&EN95=I9'1H(CH@,2P-"B`@("`@("`@("`@
M(")P;VEN=%-I>F4B.B`U+`T*("`@("`@("`@("`@(G-C86QE1&ES=')I8G5T
M:6]N(CH@>PT*("`@("`@("`@("`@("`B='EP92(Z(")L:6YE87(B#0H@("`@
M("`@("`@("!]+`T*("`@("`@("`@("`@(G-H;W=0;VEN=',B.B`B;F5V97(B
M+`T*("`@("`@("`@("`@(G-P86Y.=6QL<R(Z(&9A;'-E+`T*("`@("`@("`@
M("`@(G-T86-K:6YG(CH@>PT*("`@("`@("`@("`@("`B9W)O=7`B.B`B02(L
M#0H@("`@("`@("`@("`@(")M;V1E(CH@(FYO;F4B#0H@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@(G1H<F5S:&]L9'-3='EL92(Z('L-"B`@("`@("`@
M("`@("`@(FUO9&4B.B`B;V9F(@T*("`@("`@("`@("`@?0T*("`@("`@("`@
M('TL#0H@("`@("`@("`@(FUA<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H
M<F5S:&]L9',B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L
M#0H@("`@("`@("`@("`B<W1E<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@
M("`@("`@("`@("`@("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@
M("`@(")V86QU92(Z(&YU;&P-"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@
M("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@
M("`@("`@("`@("`@(")V86QU92(Z(#@P#0H@("`@("`@("`@("`@('T-"B`@
M("`@("`@("`@(%T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")U;FET(CH@
M(D-O<F5S(@T*("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6PT*
M("`@("`@("`@('L-"B`@("`@("`@("`@(")M871C:&5R(CH@>PT*("`@("`@
M("`@("`@("`B:60B.B`B8GE.86UE(BP-"B`@("`@("`@("`@("`@(F]P=&EO
M;G,B.B`B5F%L=64B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G!R
M;W!E<G1I97,B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@
M("`B:60B.B`B9&5C:6UA;',B+`T*("`@("`@("`@("`@("`@(")V86QU92(Z
M(#,-"B`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@
M('T-"B`@("`@("`@70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@>PT*
M("`@("`@("`B:"(Z(#<L#0H@("`@("`@(")W(CH@,3`L#0H@("`@("`@(")X
M(CH@,"P-"B`@("`@("`@(GDB.B`Q,PT*("`@("`@?2P-"B`@("`@(")I9"(Z
M(#$L#0H@("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B
M.B![#0H@("`@("`@(")L96=E;F0B.B![#0H@("`@("`@("`@(F-A;&-S(CH@
M6UTL#0H@("`@("`@("`@(F1I<W!L87E-;V1E(CH@(FQI<W0B+`T*("`@("`@
M("`@(")P;&%C96UE;G0B.B`B8F]T=&]M(BP-"B`@("`@("`@("`B<VAO=TQE
M9V5N9"(Z('1R=64-"B`@("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![
M#0H@("`@("`@("`@(FUO9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R
M="(Z(")N;VYE(@T*("`@("`@("!]#0H@("`@("!]+`T*("`@("`@(G1A<F=E
M=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L
M#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@
M(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N
M82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@
M("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-
M"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U
M<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@
M("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S
M:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@
M("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@
M("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B
M.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@
M("`@(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R
M>2(Z(")#;VYT86EN97)#<'55<V%G95-E8V]N9'-4;W1A;%QN?"!W:&5R92`D
M7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D
M0VQU<W1E<EPB7&Y\('=H97)E($QA8F5L<RYC<'4]/5PB=&]T86Q<(EQN?"!W
M:&5R92!,86)E;',N;F%M97-P86-E(#T](%PB)$YA;65S<&%C95PB7&Y\('=H
M97)E($QA8F5L<RYP;V0@/3T@7"(D4&]D7")<;GP@97AT96YD($YA;65S<&%C
M93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O
M<W1R:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I
M;F<H3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@0V]N=&%I;F5R("$](%PB
M7")<;GP@:6YV;VME('!R;VU?9&5L=&$H*5QN?"!S=6UM87)I>F4@5F%L=64]
M<W5M*%9A;'5E*2\V,"!B>2!B:6XH5&EM97-T86UP+"`V,#`P,&US*2P@4&]D
M7&Y\('!R;VIE8W0@5&EM97-T86UP+"!5<V%G93UR;W5N9"A686QU92PS*5QN
M?"!O<F1E<B!B>2!4:6UE<W1A;7`@87-C7&XB+`T*("`@("`@("`@(")Q=65R
M>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R>51Y<&4B.B`B2U%,
M(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@("`@("`@("`@(G)E
M9DED(CH@(E5S86=E(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I
M;65?<V5R:65S(@T*("`@("`@("!]+`T*("`@("`@("![#0H@("`@("`@("`@
M(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R
M:6-S(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@
M(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U
M<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1]
M(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@
M("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E
M<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@
M("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@
M("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y
M<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R
M92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@
M("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@
M("`@("!]+`T*("`@("`@("`@(")H:61E(CH@9F%L<V4L#0H@("`@("`@("`@
M(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R>2(Z
M(")+=6)E4&]D0V]N=&%I;F5R4F5S;W5R8V5297%U97-T<UQN?"!W:&5R92`D
M7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<B`]/2!<
M(B1#;'5S=&5R7")<;GP@=VAE<F4@3&%B96QS+FYA;65S<&%C92`]/2!<(B1.
M86UE<W!A8V5<(EQN?"!W:&5R92!,86)E;',N<&]D(#T](%PB)%!O9%PB7&Y\
M('=H97)E($QA8F5L<RYR97-O=7)C92`]/2!<(F-P=5PB7&Y\('!R;VIE8W0@
M5&EM97-T86UP+"!297%U97-T<SU686QU92(L#0H@("`@("`@("`@(G%U97)Y
M4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB
M+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F
M260B.B`B4F5Q=65S=',B+`T*("`@("`@("`@(")R97-U;'1&;W)M870B.B`B
M=&EM95]S97)I97,B#0H@("`@("`@('TL#0H@("`@("`@('L-"B`@("`@("`@
M("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE
M=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@
M("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S
M;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X
M='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-
M"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP
M<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*
M("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@
M("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B
M='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H
M97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@
M("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@
M("`@("`@('TL#0H@("`@("`@("`@(FAI9&4B.B!F86QS92P-"B`@("`@("`@
M("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y
M(CH@(DMU8F50;V1#;VYT86EN97)297-O=7)C94QI;6ET<UQN?"!W:&5R92`D
M7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<B`]/2!<
M(B1#;'5S=&5R7")<;GP@=VAE<F4@3&%B96QS+FYA;65S<&%C92`]/2!<(B1.
M86UE<W!A8V5<(EQN?"!W:&5R92!,86)E;',N<&]D(#T](%PB)%!O9%PB7&Y\
M('=H97)E($QA8F5L<RYR97-O=7)C92`]/2!<(F-P=5PB7&Y\('!R;VIE8W0@
M5&EM97-T86UP+"!,:6UI=',]5F%L=64B+`T*("`@("`@("`@(")Q=65R>5-O
M=7)C92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R>51Y<&4B.B`B2U%,(BP-
M"B`@("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@("`@("`@("`@(G)E9DED
M(CH@(DQI;6ET<R(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T86)L
M92(-"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z(")#4%4@
M57-A9V4B+`T*("`@("`@(G1Y<&4B.B`B=&EM97-E<FEE<R(-"B`@("!]+`T*
M("`@('L-"B`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`B='EP92(Z
M(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@
M("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@?2P-
M"B`@("`@(")F:65L9$-O;F9I9R(Z('L-"B`@("`@("`@(F1E9F%U;'1S(CH@
M>PT*("`@("`@("`@(")C;VQO<B(Z('L-"B`@("`@("`@("`@(")F:7AE9$-O
M;&]R(CH@(G)E9"(L#0H@("`@("`@("`@("`B;6]D92(Z(")F:7AE9"(-"B`@
M("`@("`@("!]+`T*("`@("`@("`@(")C=7-T;VTB.B![#0H@("`@("`@("`@
M("`B87AI<T)O<F1E<E-H;W<B.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES
M0V5N=&5R961:97)O(CH@9F%L<V4L#0H@("`@("`@("`@("`B87AI<T-O;&]R
M36]D92(Z(")T97AT(BP-"B`@("`@("`@("`@(")A>&ES3&%B96PB.B`B(BP-
M"B`@("`@("`@("`@(")A>&ES4&QA8V5M96YT(CH@(F%U=&\B+`T*("`@("`@
M("`@("`@(F%X:7-3;V9T36%X(CH@,2P-"B`@("`@("`@("`@(")A>&ES4V]F
M=$UI;B(Z(#`L#0H@("`@("`@("`@("`B8F%R06QI9VYM96YT(CH@,"P-"B`@
M("`@("`@("`@(")D<F%W4W1Y;&4B.B`B;&EN92(L#0H@("`@("`@("`@("`B
M9FEL;$]P86-I='DB.B`Q,"P-"B`@("`@("`@("`@(")G<F%D:65N=$UO9&4B
M.B`B;F]N92(L#0H@("`@("`@("`@("`B:&ED949R;VTB.B![#0H@("`@("`@
M("`@("`@(")L96=E;F0B.B!F86QS92P-"B`@("`@("`@("`@("`@(G1O;VQT
M:7`B.B!F86QS92P-"B`@("`@("`@("`@("`@(G9I>B(Z(&9A;'-E#0H@("`@
M("`@("`@("!]+`T*("`@("`@("`@("`@(FEN<V5R=$YU;&QS(CH@9F%L<V4L
M#0H@("`@("`@("`@("`B;&EN94EN=&5R<&]L871I;VXB.B`B<VUO;W1H(BP-
M"B`@("`@("`@("`@(")L:6YE5VED=&@B.B`Q+`T*("`@("`@("`@("`@(G!O
M:6YT4VEZ92(Z(#4L#0H@("`@("`@("`@("`B<V-A;&5$:7-T<FEB=71I;VXB
M.B![#0H@("`@("`@("`@("`@(")T>7!E(CH@(FQI;F5A<B(-"B`@("`@("`@
M("`@('TL#0H@("`@("`@("`@("`B<VAO=U!O:6YT<R(Z(")N979E<B(L#0H@
M("`@("`@("`@("`B<W!A;DYU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B
M<W1A8VMI;F<B.B![#0H@("`@("`@("`@("`@(")G<F]U<"(Z(")!(BP-"B`@
M("`@("`@("`@("`@(FUO9&4B.B`B;F]N92(-"B`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`B=&AR97-H;VQD<U-T>6QE(CH@>PT*("`@("`@("`@("`@
M("`B;6]D92(Z(")O9F8B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-
M"B`@("`@("`@("`B;6%P<&EN9W,B.B!;72P-"B`@("`@("`@("`B=&AR97-H
M;VQD<R(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@(F%B<V]L=71E(BP-"B`@
M("`@("`@("`@(")S=&5P<R(Z(%L-"B`@("`@("`@("`@("`@>PT*("`@("`@
M("`@("`@("`@(")C;VQO<B(Z(")G<F5E;B(L#0H@("`@("`@("`@("`@("`@
M(G9A;'5E(CH@;G5L;`T*("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(G)E9"(L#0H@("`@("`@
M("`@("`@("`@(G9A;'5E(CH@.#`-"B`@("`@("`@("`@("`@?0T*("`@("`@
M("`@("`@70T*("`@("`@("`@('TL#0H@("`@("`@("`@(G5N:70B.B`B<&5R
M8V5N='5N:70B#0H@("`@("`@('TL#0H@("`@("`@(")O=F5R<FED97,B.B!;
M70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@>PT*("`@("`@("`B:"(Z
M(#<L#0H@("`@("`@(")W(CH@.2P-"B`@("`@("`@(G@B.B`Q,"P-"B`@("`@
M("`@(GDB.B`Q,PT*("`@("`@?2P-"B`@("`@(")I9"(Z(#(L#0H@("`@("`B
M:6YT97)V86PB.B`B,S!S(BP-"B`@("`@(")O<'1I;VYS(CH@>PT*("`@("`@
M("`B;&5G96YD(CH@>PT*("`@("`@("`@(")C86QC<R(Z(%M=+`T*("`@("`@
M("`@(")D:7-P;&%Y36]D92(Z(")L:7-T(BP-"B`@("`@("`@("`B<&QA8V5M
M96YT(CH@(F)O='1O;2(L#0H@("`@("`@("`@(G-H;W=,96=E;F0B.B!T<G5E
M#0H@("`@("`@('TL#0H@("`@("`@(")T;V]L=&EP(CH@>PT*("`@("`@("`@
M(")M;V1E(CH@(G-I;F=L92(L#0H@("`@("`@("`@(G-O<G0B.B`B;F]N92(-
M"B`@("`@("`@?0T*("`@("`@?2P-"B`@("`@(")T87)G971S(CH@6PT*("`@
M("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@("`@
M(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O=7)C
M92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T
M82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B
M)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@("`@('TL#0H@("`@("`@("`@
M(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-
M"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E
M<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@
M("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G:6Y6
M97)S:6]N(CH@(C4N,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B;&5T('1H
M<F]T=&QE9#U#;VYT86EN97)#<'5#9G-4:')O='1L961097)I;V1S5&]T86Q<
M;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H97)E($QA
M8F5L<RYN86UE<W!A8V4@/3T@7"(D3F%M97-P86-E7")<;GP@=VAE<F4@3&%B
M96QS+G!O9"`]/2!<(B10;V1<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R
M:6YG*$QA8F5L<RYN86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H
M3&%B96QS+G!O9"E<;GP@97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E
M;',N8V]N=&%I;F5R*5QN?"!W:&5R92!#;VYT86EN97(@(3T@7")<(EQN?"!I
M;G9O:V4@<')O;5]D96QT82@I7&Y\(&5X=&5N9"!686QU93U686QU92\V,%QN
M?"!M86ME+7-E<FEE<R!4:')O='1L960]<W5M*%9A;'5E*2!O;B!4:6UE<W1A
M;7`@9G)O;2!B:6XH)%]?=&EM949R;VTL("1?7W1I;65);G1E<G9A;"D@=&\@
M8FEN*"1?7W1I;654;RP@)%]?=&EM94EN=&5R=F%L*2!S=&5P("1?7W1I;65)
M;G1E<G9A;"!B>2!0;V1<;GP@<')O:F5C="!0;V0L(%1I;65S=&%M<"P@5&AR
M;W1T;&5D.UQN;&5T('1O=&%L/4-O;G1A:6YE<D-P=4-F<U!E<FEO9'-4;W1A
M;%QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@
M3&%B96QS+FYA;65S<&%C92`]/2!<(B1.86UE<W!A8V5<(EQN?"!W:&5R92!,
M86)E;',N<&]D(#T](%PB)%!O9%PB7&Y\(&5X=&5N9"!.86UE<W!A8V4]=&]S
M=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@97AT96YD(%!O9#UT;W-T<FEN
M9RA,86)E;',N<&]D*5QN?"!E>'1E;F0@0V]N=&%I;F5R/71O<W1R:6YG*$QA
M8F5L<RYC;VYT86EN97(I7&Y\('=H97)E($-O;G1A:6YE<B`A/2!<(EPB7&Y\
M(&EN=F]K92!P<F]M7V1E;'1A*"E<;GP@97AT96YD(%9A;'5E/59A;'5E+S8P
M7&Y\(&UA:V4M<V5R:65S(%1O=&%L/7-U;2A686QU92D@;VX@5&EM97-T86UP
M(&9R;VT@8FEN*"1?7W1I;65&<F]M+"`D7U]T:6UE26YT97)V86PI('1O(&)I
M;B@D7U]T:6UE5&\L("1?7W1I;65);G1E<G9A;"D@<W1E<"`D7U]T:6UE26YT
M97)V86P@8GD@4&]D7&Y\('!R;VIE8W0@4&]D+"!4:6UE<W1A;7`L(%1O=&%L
M.UQN=&AR;W1T;&5D7&Y\(&IO:6X@=&]T86P@;VX@4&]D7&Y\(&5X=&5N9"!6
M86QU93US97)I97-?9&EV:61E*%1H<F]T=&QE9"P@5&]T86PI7&Y\('!R;VIE
M8W0@5&EM97-T86UP+"!.86UE/5!O9"P@5F%L=65<;GP@:6YV;VME(&=R869A
M;F%?<V5R:65S*"E<;B\O('P@97AT96YD($YA;CUS97)I97-?9FEL;%]C;VYS
M="AS97)I97-?9W)E871E<E]E<75A;',H5F%L=64L(#`I+"!B;V]L*&9A;'-E
M*2E<;B\O('P@97AT96YD($EN9CUS97)I97-?97%U86QS*%9A;'5E+"!R96%L
M*"MI;F8I*5QN+R\@?"!E>'1E;F0@5F%L=64]87)R87E?:69F*$YA;BP@5F%L
M=64L(#`I7&XO+R!\(&5X=&5N9"!686QU93UA<G)A>5]I9F8H26YF+"`P+"!6
M86QU92E<;B\O('P@<')O:F5C="UA=V%Y($YA;BP@26YF7&Y<;B(L#0H@("`@
M("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y
M5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@
M("`@("`@("`B<F5F260B.B`B57-A9V4B+`T*("`@("`@("`@(")R97-U;'1&
M;W)M870B.B`B=&EM95]S97)I97-?861X7W-E<FEE<R(-"B`@("`@("`@?0T*
M("`@("`@72P-"B`@("`@(")T:71L92(Z(")#4%4@5&AR;W1T;&EN9R(L#0H@
M("`@("`B='EP92(Z(")T:6UE<V5R:65S(@T*("`@('TL#0H@("`@>PT*("`@
M("`@(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@,2P-"B`@("`@("`@(G<B
M.B`R-"P-"B`@("`@("`@(G@B.B`P+`T*("`@("`@("`B>2(Z(#(P#0H@("`@
M("!]+`T*("`@("`@(FED(CH@."P-"B`@("`@(")T:71L92(Z(")-96UO<GDB
M+`T*("`@("`@(G1Y<&4B.B`B<F]W(@T*("`@('TL#0H@("`@>PT*("`@("`@
M(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("!]+`T*("`@("`@(F9I96QD
M0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@("`@("`@("`@
M(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B<&%L971T92UC;&%S
M<VEC(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F-U<W1O;2(Z('L-"B`@
M("`@("`@("`@(")A>&ES0F]R9&5R4VAO=R(Z(&9A;'-E+`T*("`@("`@("`@
M("`@(F%X:7-#96YT97)E9%IE<F\B.B!F86QS92P-"B`@("`@("`@("`@(")A
M>&ES0V]L;W)-;V1E(CH@(G1E>'0B+`T*("`@("`@("`@("`@(F%X:7-,86)E
M;"(Z("(B+`T*("`@("`@("`@("`@(F%X:7-0;&%C96UE;G0B.B`B875T;R(L
M#0H@("`@("`@("`@("`B8F%R06QI9VYM96YT(CH@,"P-"B`@("`@("`@("`@
M(")D<F%W4W1Y;&4B.B`B;&EN92(L#0H@("`@("`@("`@("`B9FEL;$]P86-I
M='DB.B`P+`T*("`@("`@("`@("`@(F=R861I96YT36]D92(Z(")N;VYE(BP-
M"B`@("`@("`@("`@(")H:61E1G)O;2(Z('L-"B`@("`@("`@("`@("`@(FQE
M9V5N9"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=&]O;'1I<"(Z(&9A;'-E
M+`T*("`@("`@("`@("`@("`B=FEZ(CH@9F%L<V4-"B`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`B:6YS97)T3G5L;',B.B!F86QS92P-"B`@("`@("`@
M("`@(")L:6YE26YT97)P;VQA=&EO;B(Z(")L:6YE87(B+`T*("`@("`@("`@
M("`@(FQI;F57:61T:"(Z(#$L#0H@("`@("`@("`@("`B<&]I;G13:7IE(CH@
M-2P-"B`@("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z('L-"B`@("`@
M("`@("`@("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@(")S:&]W4&]I;G1S(CH@(FYE=F5R(BP-"B`@("`@("`@("`@
M(")S<&%N3G5L;',B.B!F86QS92P-"B`@("`@("`@("`@(")S=&%C:VEN9R(Z
M('L-"B`@("`@("`@("`@("`@(F=R;W5P(CH@(D$B+`T*("`@("`@("`@("`@
M("`B;6]D92(Z(")N;VYE(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@
M(")T:')E<VAO;&1S4W1Y;&4B.B![#0H@("`@("`@("`@("`@(")M;V1E(CH@
M(F]F9B(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@
M(")M87!P:6YG<R(Z(%M=+`T*("`@("`@("`@(")T:')E<VAO;&1S(CH@>PT*
M("`@("`@("`@("`@(FUO9&4B.B`B86)S;VQU=&4B+`T*("`@("`@("`@("`@
M(G-T97!S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@
M(F-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B!N
M=6QL#0H@("`@("`@("`@("`@('TL#0H@("`@("`@("`@("`@('L-"B`@("`@
M("`@("`@("`@("`B8V]L;W(B.B`B<F5D(BP-"B`@("`@("`@("`@("`@("`B
M=F%L=64B.B`X,`T*("`@("`@("`@("`@("!]#0H@("`@("`@("`@("!=#0H@
M("`@("`@("`@?2P-"B`@("`@("`@("`B=6YI="(Z(")B>71E<R(-"B`@("`@
M("`@?2P-"B`@("`@("`@(F]V97)R:61E<R(Z(%M=#0H@("`@("!]+`T*("`@
M("`@(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@-RP-"B`@("`@("`@(G<B
M.B`Q,"P-"B`@("`@("`@(G@B.B`P+`T*("`@("`@("`B>2(Z(#(Q#0H@("`@
M("!]+`T*("`@("`@(FED(CH@-"P-"B`@("`@(")I;G1E<G9A;"(Z("(Q;2(L
M#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(FQE9V5N9"(Z('L-"B`@
M("`@("`@("`B8V%L8W,B.B!;72P-"B`@("`@("`@("`B9&ES<&QA>4UO9&4B
M.B`B;&ES="(L#0H@("`@("`@("`@(G!L86-E;65N="(Z(")B;W1T;VTB+`T*
M("`@("`@("`@(")S:&]W3&5G96YD(CH@=')U90T*("`@("`@("!]+`T*("`@
M("`@("`B=&]O;'1I<"(Z('L-"B`@("`@("`@("`B;6]D92(Z(")S:6YG;&4B
M+`T*("`@("`@("`@(")S;W)T(CH@(FYO;F4B#0H@("`@("`@('T-"B`@("`@
M('TL#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@("`@
M(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T
M<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@
M("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O
M=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT
M?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*
M("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@
M("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T
M>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE
M<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@
M("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@
M("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L
M#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<DUE;6]R>5=O<FMI;F=3
M971">71E<UQN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@
M=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\(&5X=&5N9"!.86UE<W!A
M8V4]=&]S=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@97AT96YD(%!O9#UT
M;W-T<FEN9RA,86)E;',N<&]D*5QN?"!E>'1E;F0@0V]N=&%I;F5R/71O<W1R
M:6YG*$QA8F5L<RYC;VYT86EN97(I7&Y\('=H97)E($YA;65S<&%C92`]/2!<
M(B1.86UE<W!A8V5<(EQN?"!W:&5R92!0;V0@/3T@7"(D4&]D7")<;GP@97AT
M96YD(%9A;'5E/59A;'5E7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I
M(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL('1O<W1R:6YG
M*$QA8F5L<RYI9"DL(%!O9%QN?"!P<F]J96-T(%1I;65S=&%M<"P@5V]R:VEN
M9U-E=#U686QU95QN?"!O<F1E<B!B>2!4:6UE<W1A;7`@87-C7&XB+`T*("`@
M("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R
M>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@
M("`@("`@("`@(G)E9DED(CH@(E=O<FMI;F=3970B+`T*("`@("`@("`@(")R
M97-U;'1&;W)M870B.B`B=&EM95]S97)I97,B#0H@("`@("`@('TL#0H@("`@
M("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@
M(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E
M(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A
M+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D
M>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B
M97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@
M("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@
M(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R
M961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*
M("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*
M("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S
M<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@
M("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(FAI9&4B.B!F
M86QS92P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@
M("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<DUE;6]R>4-A8VAE7&Y\('=H
M97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R
M/3U<(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT;W-T<FEN9RA,
M86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R:6YG*$QA8F5L
M<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H3&%B96QS+F-O
M;G1A:6YE<BE<;GP@=VAE<F4@3F%M97-P86-E(#T](%PB)$YA;65S<&%C95PB
M7&Y\('=H97)E(%!O9"`]/2!<(B10;V1<(EQN?"!E>'1E;F0@5F%L=64]5F%L
M=65<;GP@<W5M;6%R:7IE(%9A;'5E/6%V9RA686QU92D@8GD@8FEN*%1I;65S
M=&%M<"P@)%]?=&EM94EN=&5R=F%L*2P@=&]S=')I;F<H3&%B96QS+FED*2P@
M4&]D7&Y\('!R;VIE8W0@5&EM97-T86UP+"!#86-H93U686QU95QN?"!O<F1E
M<B!B>2!4:6UE<W1A;7`@87-C7&XB+`T*("`@("`@("`@(")Q=65R>5-O=7)C
M92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@
M("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@("`@("`@("`@(G)E9DED(CH@
M(D-A8VAE9"(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E
M<FEE<R(-"B`@("`@("`@?2P-"B`@("`@("`@>PT*("`@("`@("`@(")/<&5N
M04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L
M#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B='EP
M92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L
M#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@
M("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@
M("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N
M<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@
M("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@
M(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![
M#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@
M("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@
M?2P-"B`@("`@("`@("`B:&ED92(Z(&9A;'-E+`T*("`@("`@("`@(")P;'5G
M:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B0V]N
M=&%I;F5R365M;W)Y4G-S7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T
M86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT96YD
M($YA;65S<&%C93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E
M;F0@4&]D/71O<W1R:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN
M97(]=&]S=')I;F<H3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@3F%M97-P
M86-E(#T](%PB)$YA;65S<&%C95PB7&Y\('=H97)E(%!O9"`]/2!<(B10;V1<
M(EQN?"!E>'1E;F0@5F%L=64]5F%L=65<;GP@<W5M;6%R:7IE(%9A;'5E/6%V
M9RA686QU92D@8GD@8FEN*%1I;65S=&%M<"P@)%]?=&EM94EN=&5R=F%L*2P@
M=&]S=')I;F<H3&%B96QS+FED*2P@4&]D7&Y\('!R;VIE8W0@5&EM97-T86UP
M+"!24U,]5F%L=65<;GP@;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@
M("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E
M<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*
M("`@("`@("`@(")R969)9"(Z(")24U,B+`T*("`@("`@("`@(")R97-U;'1&
M;W)M870B.B`B=&EM95]S97)I97,B#0H@("`@("`@('TL#0H@("`@("`@('L-
M"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F1A=&%B
M87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*
M("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO
M<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S
M;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S
M<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@
M("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B
M.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B
M.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@
M("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@
M("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B
M.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@
M("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(FAI9&4B.B!F86QS92P-
M"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@
M("`@(G%U97)Y(CH@(D-O;G1A:6YE<DUE;6]R>55S86=E0GET97-<;GP@=VAE
M<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T97(]
M/5PB)$-L=7-T97)<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG*$QA
M8F5L<RYN86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B96QS
M+G!O9"E<;GP@97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E;',N8V]N
M=&%I;F5R*5QN?"!W:&5R92!.86UE<W!A8V4@/3T@7"(D3F%M97-P86-E7")<
M;GP@=VAE<F4@4&]D(#T](%PB)%!O9%PB7&Y\(&5X=&5N9"!686QU93U686QU
M95QN?"!S=6UM87)I>F4@5F%L=64]879G*%9A;'5E*2!B>2!B:6XH5&EM97-T
M86UP+"`D7U]T:6UE26YT97)V86PI+"!T;W-T<FEN9RA,86)E;',N:60I+"!0
M;V1<;GP@<')O:F5C="!4:6UE<W1A;7`L(%5S86=E/59A;'5E7&Y\(&]R9&5R
M(&)Y(%1I;65S=&%M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E
M(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@
M("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B
M57-A9V4B+`T*("`@("`@("`@(")R97-U;'1&;W)M870B.B`B=&EM95]S97)I
M97,B#0H@("`@("`@('T-"B`@("`@(%TL#0H@("`@("`B=&ET;&4B.B`B365M
M;W)Y(%5S86=E(BP-"B`@("`@(")T>7!E(CH@(G1I;65S97)I97,B#0H@("`@
M?2P-"B`@("![#0H@("`@("`B8V]L;&%P<V5D(CH@9F%L<V4L#0H@("`@("`B
M9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`Q+`T*("`@("`@("`B=R(Z(#(T
M+`T*("`@("`@("`B>"(Z(#`L#0H@("`@("`@(")Y(CH@,C@-"B`@("`@('TL
M#0H@("`@("`B:60B.B`Y+`T*("`@("`@(G!A;F5L<R(Z(%M=+`T*("`@("`@
M(G1I=&QE(CH@(DYE='=O<FLB+`T*("`@("`@(G1Y<&4B.B`B<F]W(@T*("`@
M('TL#0H@("`@>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T
M>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E
M(BP-"B`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@
M("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L
M=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO
M9&4B.B`B<&%L971T92UC;&%S<VEC(@T*("`@("`@("`@('TL#0H@("`@("`@
M("`@(F-U<W1O;2(Z('L-"B`@("`@("`@("`@(")A>&ES0F]R9&5R4VAO=R(Z
M(&9A;'-E+`T*("`@("`@("`@("`@(F%X:7-#96YT97)E9%IE<F\B.B!F86QS
M92P-"B`@("`@("`@("`@(")A>&ES0V]L;W)-;V1E(CH@(G1E>'0B+`T*("`@
M("`@("`@("`@(F%X:7-,86)E;"(Z("(B+`T*("`@("`@("`@("`@(F%X:7-0
M;&%C96UE;G0B.B`B875T;R(L#0H@("`@("`@("`@("`B8F%R06QI9VYM96YT
M(CH@,"P-"B`@("`@("`@("`@(")D<F%W4W1Y;&4B.B`B;&EN92(L#0H@("`@
M("`@("`@("`B9FEL;$]P86-I='DB.B`Q,"P-"B`@("`@("`@("`@(")G<F%D
M:65N=$UO9&4B.B`B;F]N92(L#0H@("`@("`@("`@("`B:&ED949R;VTB.B![
M#0H@("`@("`@("`@("`@(")L96=E;F0B.B!F86QS92P-"B`@("`@("`@("`@
M("`@(G1O;VQT:7`B.B!F86QS92P-"B`@("`@("`@("`@("`@(G9I>B(Z(&9A
M;'-E#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(FEN<V5R=$YU;&QS
M(CH@9F%L<V4L#0H@("`@("`@("`@("`B;&EN94EN=&5R<&]L871I;VXB.B`B
M;&EN96%R(BP-"B`@("`@("`@("`@(")L:6YE5VED=&@B.B`Q+`T*("`@("`@
M("`@("`@(G!O:6YT4VEZ92(Z(#4L#0H@("`@("`@("`@("`B<V-A;&5$:7-T
M<FEB=71I;VXB.B![#0H@("`@("`@("`@("`@(")T>7!E(CH@(FQI;F5A<B(-
M"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<VAO=U!O:6YT<R(Z(")N
M979E<B(L#0H@("`@("`@("`@("`B<W!A;DYU;&QS(CH@9F%L<V4L#0H@("`@
M("`@("`@("`B<W1A8VMI;F<B.B![#0H@("`@("`@("`@("`@(")G<F]U<"(Z
M(")!(BP-"B`@("`@("`@("`@("`@(FUO9&4B.B`B;F]N92(-"B`@("`@("`@
M("`@('TL#0H@("`@("`@("`@("`B=&AR97-H;VQD<U-T>6QE(CH@>PT*("`@
M("`@("`@("`@("`B;6]D92(Z(")O9F8B#0H@("`@("`@("`@("!]#0H@("`@
M("`@("`@?2P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;72P-"B`@("`@("`@
M("`B=&AR97-H;VQD<R(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@(F%B<V]L
M=71E(BP-"B`@("`@("`@("`@(")S=&5P<R(Z(%L-"B`@("`@("`@("`@("`@
M>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")G<F5E;B(L#0H@("`@("`@
M("`@("`@("`@(G9A;'5E(CH@;G5L;`T*("`@("`@("`@("`@("!]+`T*("`@
M("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(G)E9"(L
M#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@.#`-"B`@("`@("`@("`@("`@
M?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@("`@("`@("`@(G5N
M:70B.B`B8GET97,B#0H@("`@("`@('TL#0H@("`@("`@(")O=F5R<FED97,B
M.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@>PT*("`@("`@("`B
M:"(Z(#DL#0H@("`@("`@(")W(CH@,3`L#0H@("`@("`@(")X(CH@,"P-"B`@
M("`@("`@(GDB.B`R.0T*("`@("`@?2P-"B`@("`@(")I9"(Z(#4L#0H@("`@
M("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@
M("`@(")L96=E;F0B.B![#0H@("`@("`@("`@(F-A;&-S(CH@6UTL#0H@("`@
M("`@("`@(F1I<W!L87E-;V1E(CH@(FQI<W0B+`T*("`@("`@("`@(")P;&%C
M96UE;G0B.B`B8F]T=&]M(BP-"B`@("`@("`@("`B<VAO=TQE9V5N9"(Z('1R
M=64-"B`@("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@
M("`@(FUO9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R="(Z(")N;VYE
M(@T*("`@("`@("!]#0H@("`@("!]+`T*("`@("`@(G1A<F=E=',B.B!;#0H@
M("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@
M("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U
M<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD
M871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@
M("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*
M("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@
M("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@
M(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=
M+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP
M<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*
M("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(G!L=6=I
M;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R>2(Z(")#;VYT
M86EN97).971W;W)K4F5C96EV94)Y=&5S5&]T86Q<;GP@=VAE<F4@)%]?=&EM
M949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T
M97)<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG*$QA8F5L<RYN86UE
M<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B96QS+G!O9"E<;GP@
M97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E;',N8V]N=&%I;F5R*5QN
M?"!W:&5R92!.86UE<W!A8V4@/3T@7"(D3F%M97-P86-E7")<;GP@=VAE<F4@
M4&]D(#T](%PB)%!O9%PB7&Y\(&EN=F]K92!P<F]M7V1E;'1A*"E<;GP@97AT
M96YD(%9A;'5E/59A;'5E+S8P7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L
M=64I*BTQ(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL('1O
M<W1R:6YG*$QA8F5L<RYI9"DL(%!O9%QN?"!P<F]J96-T(%1I;65S=&%M<"P@
M4F5C:65V93U686QU95QN?"!O<F1E<B!B>2!4:6UE<W1A;7`@87-C7&XB+`T*
M("`@("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@(")Q
M=65R>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R=64L
M#0H@("`@("`@("`@(G)E9DED(CH@(E)E8V5I=F4@0GET97,B+`T*("`@("`@
M("`@(")R97-U;'1&;W)M870B.B`B=&EM95]S97)I97,B#0H@("`@("`@('TL
M#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@
M("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A
M<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R
M92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I
M9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@
M("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@
M>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@
M("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@
M("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z
M(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@
M("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B
M97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD
M(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(FAI
M9&4B.B!F86QS92P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N
M,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<DYE='=O<FM4<F%N
M<VUI=$)Y=&5S5&]T86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A
M;7`I7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!E>'1E;F0@
M3F%M97-P86-E/71O<W1R:6YG*$QA8F5L<RYN86UE<W!A8V4I7&Y\(&5X=&5N
M9"!0;V0]=&]S=')I;F<H3&%B96QS+G!O9"E<;GP@97AT96YD($-O;G1A:6YE
M<CUT;W-T<FEN9RA,86)E;',N8V]N=&%I;F5R*5QN?"!W:&5R92!.86UE<W!A
M8V4@/3T@7"(D3F%M97-P86-E7")<;GP@=VAE<F4@4&]D(#T](%PB)%!O9%PB
M7&Y\(&EN=F]K92!P<F]M7V1E;'1A*"E<;GP@97AT96YD(%9A;'5E/59A;'5E
M+S8P7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I(&)Y(&)I;BA4:6UE
M<W1A;7`L("1?7W1I;65);G1E<G9A;"DL('1O<W1R:6YG*$QA8F5L<RYI9"DL
M(%!O9%QN?"!P<F]J96-T(%1I;65S=&%M<"P@5')A;G-M:70]5F%L=65<;GP@
M;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B<75E<GE3
M;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L
M#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)
M9"(Z(")!(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?<V5R
M:65S(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1I=&QE(CH@(DYE
M='=O<FL@5&AR;W5G:'!U="(L#0H@("`@("`B='EP92(Z(")T:6UE<V5R:65S
M(@T*("`@('TL#0H@("`@>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@
M("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A
M<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB
M#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B
M9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@
M("`@(FUO9&4B.B`B<&%L971T92UC;&%S<VEC(@T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(F-U<W1O;2(Z('L-"B`@("`@("`@("`@(")A>&ES0F]R9&5R
M4VAO=R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X:7-#96YT97)E9%IE<F\B
M.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES0V]L;W)-;V1E(CH@(G1E>'0B
M+`T*("`@("`@("`@("`@(F%X:7-,86)E;"(Z("(B+`T*("`@("`@("`@("`@
M(F%X:7-0;&%C96UE;G0B.B`B875T;R(L#0H@("`@("`@("`@("`B8F%R06QI
M9VYM96YT(CH@,"P-"B`@("`@("`@("`@(")D<F%W4W1Y;&4B.B`B;&EN92(L
M#0H@("`@("`@("`@("`B9FEL;$]P86-I='DB.B`Q,"P-"B`@("`@("`@("`@
M(")G<F%D:65N=$UO9&4B.B`B;F]N92(L#0H@("`@("`@("`@("`B:&ED949R
M;VTB.B![#0H@("`@("`@("`@("`@(")L96=E;F0B.B!F86QS92P-"B`@("`@
M("`@("`@("`@(G1O;VQT:7`B.B!F86QS92P-"B`@("`@("`@("`@("`@(G9I
M>B(Z(&9A;'-E#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(FEN<V5R
M=$YU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B;&EN94EN=&5R<&]L871I
M;VXB.B`B;&EN96%R(BP-"B`@("`@("`@("`@(")L:6YE5VED=&@B.B`Q+`T*
M("`@("`@("`@("`@(G!O:6YT4VEZ92(Z(#4L#0H@("`@("`@("`@("`B<V-A
M;&5$:7-T<FEB=71I;VXB.B![#0H@("`@("`@("`@("`@(")T>7!E(CH@(FQI
M;F5A<B(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<VAO=U!O:6YT
M<R(Z(")N979E<B(L#0H@("`@("`@("`@("`B<W!A;DYU;&QS(CH@9F%L<V4L
M#0H@("`@("`@("`@("`B<W1A8VMI;F<B.B![#0H@("`@("`@("`@("`@(")G
M<F]U<"(Z(")!(BP-"B`@("`@("`@("`@("`@(FUO9&4B.B`B;F]N92(-"B`@
M("`@("`@("`@('TL#0H@("`@("`@("`@("`B=&AR97-H;VQD<U-T>6QE(CH@
M>PT*("`@("`@("`@("`@("`B;6]D92(Z(")O9F8B#0H@("`@("`@("`@("!]
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;72P-"B`@
M("`@("`@("`B=&AR97-H;VQD<R(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@
M(F%B<V]L=71E(BP-"B`@("`@("`@("`@(")S=&5P<R(Z(%L-"B`@("`@("`@
M("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")G<F5E;B(L#0H@
M("`@("`@("`@("`@("`@(G9A;'5E(CH@;G5L;`T*("`@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@
M(G)E9"(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@.#`-"B`@("`@("`@
M("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@("`@("`@
M("`@(G5N:70B.B`B;F]N92(-"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R
M:61E<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@
M("`@(")H(CH@.2P-"B`@("`@("`@(G<B.B`Y+`T*("`@("`@("`B>"(Z(#$P
M+`T*("`@("`@("`B>2(Z(#(Y#0H@("`@("!]+`T*("`@("`@(FED(CH@-BP-
M"B`@("`@(")I;G1E<G9A;"(Z("(Q;2(L#0H@("`@("`B;W!T:6]N<R(Z('L-
M"B`@("`@("`@(FQE9V5N9"(Z('L-"B`@("`@("`@("`B8V%L8W,B.B!;72P-
M"B`@("`@("`@("`B9&ES<&QA>4UO9&4B.B`B;&ES="(L#0H@("`@("`@("`@
M(G!L86-E;65N="(Z(")B;W1T;VTB+`T*("`@("`@("`@(")S:&]W3&5G96YD
M(CH@=')U90T*("`@("`@("!]+`T*("`@("`@("`B=&]O;'1I<"(Z('L-"B`@
M("`@("`@("`B;6]D92(Z(")S:6YG;&4B+`T*("`@("`@("`@(")S;W)T(CH@
M(FYO;F4B#0H@("`@("`@('T-"B`@("`@('TL#0H@("`@("`B=&%R9V5T<R(Z
M(%L-"B`@("`@("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@
M("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A
M=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z
M=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B
M=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@
M("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB
M.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@
M("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@
M("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS
M(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@
M("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@
M(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A
M;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B
M<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y(CH@
M(D-O;G1A:6YE<DYE='=O<FM296-E:79E4&%C:V5T<U1O=&%L7&Y\('=H97)E
M("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<
M(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT;W-T<FEN9RA,86)E
M;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R:6YG*$QA8F5L<RYP
M;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H3&%B96QS+F-O;G1A
M:6YE<BE<;GP@=VAE<F4@3F%M97-P86-E(#T](%PB)$YA;65S<&%C95PB7&Y\
M('=H97)E(%!O9"`]/2!<(B10;V1<(EQN?"!I;G9O:V4@<')O;5]D96QT82@I
M7&Y\(&5X=&5N9"!686QU93U686QU92\V,%QN?"!S=6UM87)I>F4@5F%L=64]
M879G*%9A;'5E*2HM,2!B>2!B:6XH5&EM97-T86UP+"`D7U]T:6UE26YT97)V
M86PI+"!T;W-T<FEN9RA,86)E;',N:60I+"!0;V1<;GP@<')O:F5C="!4:6UE
M<W1A;7`L(%)E8VEE=F4]5F%L=65<;GP@;W)D97(@8GD@5&EM97-T86UP(&%S
M8UQN(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@
M("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B
M.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")296-E:79E($)Y=&5S(BP-
M"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?<V5R:65S(@T*("`@
M("`@("!]+`T*("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E
M+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@
M("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A
M;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@
M("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@("`@('TL
M#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O
M=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@
M("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S
M<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@
M("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@
M("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E
M(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@
M("`@(")H:61E(CH@9F%L<V4L#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB
M.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R>2(Z(")#;VYT86EN97).971W
M;W)K5')A;G-M:71086-K971S5&]T86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E
M<BA4:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN
M?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG*$QA8F5L<RYN86UE<W!A8V4I
M7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B96QS+G!O9"E<;GP@97AT96YD
M($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E;',N8V]N=&%I;F5R*5QN?"!W:&5R
M92!.86UE<W!A8V4@/3T@7"(D3F%M97-P86-E7")<;GP@=VAE<F4@4&]D(#T]
M(%PB)%!O9%PB7&Y\(&EN=F]K92!P<F]M7V1E;'1A*"E<;GP@97AT96YD(%9A
M;'5E/59A;'5E+S8P7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I(&)Y
M(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL('1O<W1R:6YG*$QA
M8F5L<RYI9"DL(%!O9%QN?"!P<F]J96-T(%1I;65S=&%M<"P@5')A;G-M:70]
M5F%L=65<;GP@;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@
M("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E
M(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@
M("`@(")R969)9"(Z(")!(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@
M(G1I;65?<V5R:65S(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1I
M=&QE(CH@(DYE='=O<FL@4&%C:V5T<R(L#0H@("`@("`B='EP92(Z(")T:6UE
M<V5R:65S(@T*("`@('TL#0H@("`@>PT*("`@("`@(F1A=&%S;W5R8V4B.B![
M#0H@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E
M<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z
M=&5X='TB#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@
M("`@("`B9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@
M("`@("`@("`@(FUO9&4B.B`B<&%L971T92UC;&%S<VEC(@T*("`@("`@("`@
M('TL#0H@("`@("`@("`@(F-U<W1O;2(Z('L-"B`@("`@("`@("`@(")A>&ES
M0F]R9&5R4VAO=R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X:7-#96YT97)E
M9%IE<F\B.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES0V]L;W)-;V1E(CH@
M(G1E>'0B+`T*("`@("`@("`@("`@(F%X:7-,86)E;"(Z("(B+`T*("`@("`@
M("`@("`@(F%X:7-0;&%C96UE;G0B.B`B875T;R(L#0H@("`@("`@("`@("`B
M8F%R06QI9VYM96YT(CH@,"P-"B`@("`@("`@("`@(")D<F%W4W1Y;&4B.B`B
M;&EN92(L#0H@("`@("`@("`@("`B9FEL;$]P86-I='DB.B`Q,"P-"B`@("`@
M("`@("`@(")G<F%D:65N=$UO9&4B.B`B;F]N92(L#0H@("`@("`@("`@("`B
M:&ED949R;VTB.B![#0H@("`@("`@("`@("`@(")L96=E;F0B.B!F86QS92P-
M"B`@("`@("`@("`@("`@(G1O;VQT:7`B.B!F86QS92P-"B`@("`@("`@("`@
M("`@(G9I>B(Z(&9A;'-E#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(FEN<V5R=$YU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B;&EN94EN=&5R
M<&]L871I;VXB.B`B;&EN96%R(BP-"B`@("`@("`@("`@(")L:6YE5VED=&@B
M.B`Q+`T*("`@("`@("`@("`@(G!O:6YT4VEZ92(Z(#4L#0H@("`@("`@("`@
M("`B<V-A;&5$:7-T<FEB=71I;VXB.B![#0H@("`@("`@("`@("`@(")T>7!E
M(CH@(FQI;F5A<B(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<VAO
M=U!O:6YT<R(Z(")N979E<B(L#0H@("`@("`@("`@("`B<W!A;DYU;&QS(CH@
M9F%L<V4L#0H@("`@("`@("`@("`B<W1A8VMI;F<B.B![#0H@("`@("`@("`@
M("`@(")G<F]U<"(Z(")!(BP-"B`@("`@("`@("`@("`@(FUO9&4B.B`B;F]N
M92(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=&AR97-H;VQD<U-T
M>6QE(CH@>PT*("`@("`@("`@("`@("`B;6]D92(Z(")O9F8B#0H@("`@("`@
M("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;
M72P-"B`@("`@("`@("`B=&AR97-H;VQD<R(Z('L-"B`@("`@("`@("`@(")M
M;V1E(CH@(F%B<V]L=71E(BP-"B`@("`@("`@("`@(")S=&5P<R(Z(%L-"B`@
M("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")G<F5E
M;B(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@;G5L;`T*("`@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O
M;&]R(CH@(G)E9"(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@.#`-"B`@
M("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(G5N:70B.B`B;F]N92(-"B`@("`@("`@?2P-"B`@("`@("`@
M(F]V97)R:61E<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![
M#0H@("`@("`@(")H(CH@.2P-"B`@("`@("`@(G<B.B`Q,"P-"B`@("`@("`@
M(G@B.B`P+`T*("`@("`@("`B>2(Z(#,X#0H@("`@("!]+`T*("`@("`@(FED
M(CH@,3`L#0H@("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO
M;G,B.B![#0H@("`@("`@(")L96=E;F0B.B![#0H@("`@("`@("`@(F-A;&-S
M(CH@6UTL#0H@("`@("`@("`@(F1I<W!L87E-;V1E(CH@(FQI<W0B+`T*("`@
M("`@("`@(")P;&%C96UE;G0B.B`B8F]T=&]M(BP-"B`@("`@("`@("`B<VAO
M=TQE9V5N9"(Z('1R=64-"B`@("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B
M.B![#0H@("`@("`@("`@(FUO9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B
M<V]R="(Z(")N;VYE(@T*("`@("`@("!]#0H@("`@("!]+`T*("`@("`@(G1A
M<F=E=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L
M<V4L#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@
M("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A
M9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@
M("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@
M?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G
M<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-
M"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@
M("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y
M<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@
M("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q
M=65R>2(Z(")#;VYT86EN97).971W;W)K4F5C96EV945R<F]R<U1O=&%L7&Y\
M('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S
M=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT;W-T<FEN
M9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R:6YG*$QA
M8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H3&%B96QS
M+F-O;G1A:6YE<BE<;GP@=VAE<F4@3F%M97-P86-E(#T](%PB)$YA;65S<&%C
M95PB7&Y\('=H97)E(%!O9"`]/2!<(B10;V1<(EQN?"!I;G9O:V4@<')O;5]D
M96QT82@I7&Y\(&5X=&5N9"!686QU93U686QU92\V,%QN?"!S=6UM87)I>F4@
M5F%L=64]879G*%9A;'5E*2HM,2!B>2!B:6XH5&EM97-T86UP+"`D7U]T:6UE
M26YT97)V86PI+"!T;W-T<FEN9RA,86)E;',N:60I+"!0;V1<;GP@<')O:F5C
M="!4:6UE<W1A;7`L(%)E8VEE=F4]5F%L=65<;GP@;W)D97(@8GD@5&EM97-T
M86UP(&%S8UQN(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-
M"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A
M=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")296-E:79E($)Y
M=&5S(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?<V5R:65S
M(@T*("`@("`@("!]+`T*("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z
M(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@
M("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@
M(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@
M("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@
M("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@
M("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@
M6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@
M('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B
M97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD
M(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*
M("`@("`@("`@(")H:61E(CH@9F%L<V4L#0H@("`@("`@("`@(G!L=6=I;E9E
M<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R>2(Z(")#;VYT86EN
M97).971W;W)K5')A;G-M:71%<G)O<G-4;W1A;%QN?"!W:&5R92`D7U]T:6UE
M1FEL=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E
M<EPB7&Y\(&5X=&5N9"!.86UE<W!A8V4]=&]S=')I;F<H3&%B96QS+FYA;65S
M<&%C92E<;GP@97AT96YD(%!O9#UT;W-T<FEN9RA,86)E;',N<&]D*5QN?"!E
M>'1E;F0@0V]N=&%I;F5R/71O<W1R:6YG*$QA8F5L<RYC;VYT86EN97(I7&Y\
M('=H97)E($YA;65S<&%C92`]/2!<(B1.86UE<W!A8V5<(EQN?"!W:&5R92!0
M;V0@/3T@7"(D4&]D7")<;GP@:6YV;VME('!R;VU?9&5L=&$H*5QN?"!E>'1E
M;F0@5F%L=64]5F%L=64O-C!<;GP@<W5M;6%R:7IE(%9A;'5E/6%V9RA686QU
M92D@8GD@8FEN*%1I;65S=&%M<"P@)%]?=&EM94EN=&5R=F%L*2P@=&]S=')I
M;F<H3&%B96QS+FED*2P@4&]D7&Y\('!R;VIE8W0@5&EM97-T86UP+"!4<F%N
M<VUI=#U686QU95QN?"!O<F1E<B!B>2!4:6UE<W1A;7`@87-C7&XB+`T*("`@
M("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R
M>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@
M("`@("`@("`@(G)E9DED(CH@(D$B+`T*("`@("`@("`@(")R97-U;'1&;W)M
M870B.B`B=&EM95]S97)I97,B#0H@("`@("`@('T-"B`@("`@(%TL#0H@("`@
M("`B=&ET;&4B.B`B3F5T=V]R:R!%<G)O<G,B+`T*("`@("`@(G1Y<&4B.B`B
M=&EM97-E<FEE<R(-"B`@("!]+`T*("`@('L-"B`@("`@(")D871A<V]U<F-E
M(CH@>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP
M;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U
M<F-E.G1E>'1](@T*("`@("`@?2P-"B`@("`@(")F:65L9$-O;F9I9R(Z('L-
M"B`@("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@(")C;VQO<B(Z('L-
M"B`@("`@("`@("`@(")M;V1E(CH@(G!A;&5T=&4M8VQA<W-I8R(-"B`@("`@
M("`@("!]+`T*("`@("`@("`@(")C=7-T;VTB.B![#0H@("`@("`@("`@("`B
M87AI<T)O<F1E<E-H;W<B.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES0V5N
M=&5R961:97)O(CH@9F%L<V4L#0H@("`@("`@("`@("`B87AI<T-O;&]R36]D
M92(Z(")T97AT(BP-"B`@("`@("`@("`@(")A>&ES3&%B96PB.B`B(BP-"B`@
M("`@("`@("`@(")A>&ES4&QA8V5M96YT(CH@(F%U=&\B+`T*("`@("`@("`@
M("`@(F)A<D%L:6=N;65N="(Z(#`L#0H@("`@("`@("`@("`B9')A=U-T>6QE
M(CH@(FQI;F4B+`T*("`@("`@("`@("`@(F9I;&Q/<&%C:71Y(CH@,3`L#0H@
M("`@("`@("`@("`B9W)A9&EE;G1-;V1E(CH@(FYO;F4B+`T*("`@("`@("`@
M("`@(FAI9&5&<F]M(CH@>PT*("`@("`@("`@("`@("`B;&5G96YD(CH@9F%L
M<V4L#0H@("`@("`@("`@("`@(")T;V]L=&EP(CH@9F%L<V4L#0H@("`@("`@
M("`@("`@(")V:7HB.B!F86QS90T*("`@("`@("`@("`@?2P-"B`@("`@("`@
M("`@(")I;G-E<G1.=6QL<R(Z(&9A;'-E+`T*("`@("`@("`@("`@(FQI;F5)
M;G1E<G!O;&%T:6]N(CH@(FQI;F5A<B(L#0H@("`@("`@("`@("`B;&EN95=I
M9'1H(CH@,2P-"B`@("`@("`@("`@(")P;VEN=%-I>F4B.B`U+`T*("`@("`@
M("`@("`@(G-C86QE1&ES=')I8G5T:6]N(CH@>PT*("`@("`@("`@("`@("`B
M='EP92(Z(")L:6YE87(B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(G-H;W=0;VEN=',B.B`B;F5V97(B+`T*("`@("`@("`@("`@(G-P86Y.=6QL
M<R(Z(&9A;'-E+`T*("`@("`@("`@("`@(G-T86-K:6YG(CH@>PT*("`@("`@
M("`@("`@("`B9W)O=7`B.B`B02(L#0H@("`@("`@("`@("`@(")M;V1E(CH@
M(FYO;F4B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G1H<F5S:&]L
M9'-3='EL92(Z('L-"B`@("`@("`@("`@("`@(FUO9&4B.B`B;V9F(@T*("`@
M("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(FUA<'!I;F=S
M(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@("`@("`@("`@
M("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B<W1E<',B.B!;
M#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B
M9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(&YU;&P-"B`@("`@
M("`@("`@("`@?2P-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@
M(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(#@P
M#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@("`@("!]
M+`T*("`@("`@("`@(")U;FET(CH@(FYO;F4B#0H@("`@("`@('TL#0H@("`@
M("`@(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S
M(CH@>PT*("`@("`@("`B:"(Z(#DL#0H@("`@("`@(")W(CH@.2P-"B`@("`@
M("`@(G@B.B`Q,"P-"B`@("`@("`@(GDB.B`S.`T*("`@("`@?2P-"B`@("`@
M(")I9"(Z(#$Q+`T*("`@("`@(FEN=&5R=F%L(CH@(C%M(BP-"B`@("`@(")O
M<'1I;VYS(CH@>PT*("`@("`@("`B;&5G96YD(CH@>PT*("`@("`@("`@(")C
M86QC<R(Z(%M=+`T*("`@("`@("`@(")D:7-P;&%Y36]D92(Z(")L:7-T(BP-
M"B`@("`@("`@("`B<&QA8V5M96YT(CH@(F)O='1O;2(L#0H@("`@("`@("`@
M(G-H;W=,96=E;F0B.B!T<G5E#0H@("`@("`@('TL#0H@("`@("`@(")T;V]L
M=&EP(CH@>PT*("`@("`@("`@(")M;V1E(CH@(G-I;F=L92(L#0H@("`@("`@
M("`@(G-O<G0B.B`B;F]N92(-"B`@("`@("`@?0T*("`@("`@?2P-"B`@("`@
M(")T87)G971S(CH@6PT*("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z
M(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@
M("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@
M(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@
M("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@
M("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@
M("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@
M6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@
M('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B
M97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD
M(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*
M("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@("`@("`@
M("`B<75E<GDB.B`B0V]N=&%I;F5R3F5T=V]R:U)E8V5I=F5086-K971S1')O
M<'!E9%1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN
M?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S
M<&%C93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D
M/71O<W1R:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S
M=')I;F<H3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@3F%M97-P86-E(#T]
M(%PB)$YA;65S<&%C95PB7&Y\('=H97)E(%!O9"`]/2!<(B10;V1<(EQN?"!I
M;G9O:V4@<')O;5]D96QT82@I7&Y\(&5X=&5N9"!686QU93U686QU92\V,%QN
M?"!S=6UM87)I>F4@5F%L=64]879G*%9A;'5E*2HM,2!B>2!B:6XH5&EM97-T
M86UP+"`D7U]T:6UE26YT97)V86PI+"!T;W-T<FEN9RA,86)E;',N:60I+"!0
M;V1<;GP@<')O:F5C="!4:6UE<W1A;7`L(%)E8VEE=F4]5F%L=65<;GP@;W)D
M97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B<75E<GE3;W5R
M8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@
M("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z
M(")296-E:79E($)Y=&5S(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@
M(G1I;65?<V5R:65S(@T*("`@("`@("!]+`T*("`@("`@("![#0H@("`@("`@
M("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z(")-
M971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@
M("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A
M<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E
M>'1](@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![
M#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X
M<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-
M"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@
M("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@
M(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W
M:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@
M("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@
M("`@("`@("!]+`T*("`@("`@("`@(")H:61E(CH@9F%L<V4L#0H@("`@("`@
M("`@(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R
M>2(Z(")#;VYT86EN97).971W;W)K5')A;G-M:71086-K971S1')O<'!E9%1O
M=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R
M92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT
M;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R
M:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H
M3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@3F%M97-P86-E(#T](%PB)$YA
M;65S<&%C95PB7&Y\('=H97)E(%!O9"`]/2!<(B10;V1<(EQN?"!I;G9O:V4@
M<')O;5]D96QT82@I7&Y\(&5X=&5N9"!686QU93U686QU92\V,%QN?"!S=6UM
M87)I>F4@5F%L=64]879G*%9A;'5E*2!B>2!B:6XH5&EM97-T86UP+"`D7U]T
M:6UE26YT97)V86PI+"!T;W-T<FEN9RA,86)E;',N:60I+"!0;V1<;GP@<')O
M:F5C="!4:6UE<W1A;7`L(%1R86YS;6ET/59A;'5E7&Y\(&]R9&5R(&)Y(%1I
M;65S=&%M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A
M=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@
M(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@
M("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@
M("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z(").971W;W)K($1R;W!P
M960@4&%C:V5T<R(L#0H@("`@("`B='EP92(Z(")T:6UE<V5R:65S(@T*("`@
M('TL#0H@("`@>PT*("`@("`@(F-O;&QA<'-E9"(Z(&9A;'-E+`T*("`@("`@
M(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@,2P-"B`@("`@("`@(G<B.B`R
M-"P-"B`@("`@("`@(G@B.B`P+`T*("`@("`@("`B>2(Z(#0W#0H@("`@("!]
M+`T*("`@("`@(FED(CH@,3(L#0H@("`@("`B<&%N96QS(CH@6UTL#0H@("`@
M("`B=&ET;&4B.B`B1&ES:R!)3R(L#0H@("`@("`B='EP92(Z(")R;W<B#0H@
M("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@
M(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R
M8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@
M("`@('TL#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@("`@(")D969A
M=6QT<R(Z('L-"B`@("`@("`@("`B8V]L;W(B.B![#0H@("`@("`@("`@("`B
M;6]D92(Z(")P86QE='1E+6-L87-S:6,B#0H@("`@("`@("`@?2P-"B`@("`@
M("`@("`B8W5S=&]M(CH@>PT*("`@("`@("`@("`@(F%X:7-";W)D97)3:&]W
M(CH@9F%L<V4L#0H@("`@("`@("`@("`B87AI<T-E;G1E<F5D6F5R;R(Z(&9A
M;'-E+`T*("`@("`@("`@("`@(F%X:7-#;VQO<DUO9&4B.B`B=&5X="(L#0H@
M("`@("`@("`@("`B87AI<TQA8F5L(CH@(B(L#0H@("`@("`@("`@("`B87AI
M<U!L86-E;65N="(Z(")A=71O(BP-"B`@("`@("`@("`@(")B87)!;&EG;FUE
M;G0B.B`P+`T*("`@("`@("`@("`@(F1R87=3='EL92(Z(")L:6YE(BP-"B`@
M("`@("`@("`@(")F:6QL3W!A8VET>2(Z(#$P+`T*("`@("`@("`@("`@(F=R
M861I96YT36]D92(Z(")N;VYE(BP-"B`@("`@("`@("`@(")H:61E1G)O;2(Z
M('L-"B`@("`@("`@("`@("`@(FQE9V5N9"(Z(&9A;'-E+`T*("`@("`@("`@
M("`@("`B=&]O;'1I<"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=FEZ(CH@
M9F%L<V4-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B:6YS97)T3G5L
M;',B.B!F86QS92P-"B`@("`@("`@("`@(")L:6YE26YT97)P;VQA=&EO;B(Z
M(")L:6YE87(B+`T*("`@("`@("`@("`@(FQI;F57:61T:"(Z(#$L#0H@("`@
M("`@("`@("`B<&]I;G13:7IE(CH@-2P-"B`@("`@("`@("`@(")S8V%L941I
M<W1R:6)U=&EO;B(Z('L-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B;&EN96%R
M(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")S:&]W4&]I;G1S(CH@
M(FYE=F5R(BP-"B`@("`@("`@("`@(")S<&%N3G5L;',B.B!F86QS92P-"B`@
M("`@("`@("`@(")S=&%C:VEN9R(Z('L-"B`@("`@("`@("`@("`@(F=R;W5P
M(CH@(D$B+`T*("`@("`@("`@("`@("`B;6]D92(Z(")N;VYE(@T*("`@("`@
M("`@("`@?2P-"B`@("`@("`@("`@(")T:')E<VAO;&1S4W1Y;&4B.B![#0H@
M("`@("`@("`@("`@(")M;V1E(CH@(F]F9B(-"B`@("`@("`@("`@('T-"B`@
M("`@("`@("!]+`T*("`@("`@("`@(")M87!P:6YG<R(Z(%M=+`T*("`@("`@
M("`@(")T:')E<VAO;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B86)S
M;VQU=&4B+`T*("`@("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@("`@
M("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@("`@
M("`@("`@("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B<F5D
M(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*("`@("`@("`@("`@
M("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B
M=6YI="(Z(")B>71E<R(-"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R:61E
M<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@("`@
M(")H(CH@-RP-"B`@("`@("`@(G<B.B`Q,"P-"B`@("`@("`@(G@B.B`P+`T*
M("`@("`@("`B>2(Z(#0X#0H@("`@("!]+`T*("`@("`@(FED(CH@,3,L#0H@
M("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B.B![#0H@
M("`@("`@(")L96=E;F0B.B![#0H@("`@("`@("`@(F-A;&-S(CH@6UTL#0H@
M("`@("`@("`@(F1I<W!L87E-;V1E(CH@(FQI<W0B+`T*("`@("`@("`@(")P
M;&%C96UE;G0B.B`B8F]T=&]M(BP-"B`@("`@("`@("`B<VAO=TQE9V5N9"(Z
M('1R=64-"B`@("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@("`@
M("`@("`@(FUO9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R="(Z(")N
M;VYE(@T*("`@("`@("!]#0H@("`@("!]+`T*("`@("`@(G1A<F=E=',B.B!;
M#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@
M("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A
M<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R
M92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I
M9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@
M("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@
M>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@
M("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@
M("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z
M(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@
M("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B
M97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD
M(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(G!L
M=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R>2(Z(")#
M;VYT86EN97)&<U)E861S0GET97-4;W1A;%QN?"!W:&5R92`D7U]T:6UE1FEL
M=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB
M7&Y\(&5X=&5N9"!.86UE<W!A8V4]=&]S=')I;F<H3&%B96QS+FYA;65S<&%C
M92E<;GP@97AT96YD(%!O9#UT;W-T<FEN9RA,86)E;',N<&]D*5QN?"!E>'1E
M;F0@0V]N=&%I;F5R/71O<W1R:6YG*$QA8F5L<RYC;VYT86EN97(I7&Y\('=H
M97)E($YA;65S<&%C92`]/2!<(B1.86UE<W!A8V5<(EQN?"!W:&5R92!0;V0@
M/3T@7"(D4&]D7")<;GP@:6YV;VME('!R;VU?9&5L=&$H*5QN?"!E>'1E;F0@
M5F%L=64]5F%L=64O-C!<;GP@<W5M;6%R:7IE(%9A;'5E/6%V9RA686QU92DJ
M+3$@8GD@8FEN*%1I;65S=&%M<"P@)%]?=&EM94EN=&5R=F%L*2P@=&]S=')I
M;F<H3&%B96QS+FED*2P@4&]D7&Y\('!R;VIE8W0@5&EM97-T86UP+"!296%D
M/59A;'5E7&Y\(&]R9&5R(&)Y(%1I;65S=&%M<"!A<V-<;B(L#0H@("`@("`@
M("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP
M92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@
M("`@("`B<F5F260B.B`B4F5C96EV92!">71E<R(L#0H@("`@("`@("`@(G)E
M<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@("`@?2P-"B`@("`@
M("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B
M9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B
M.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M
M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[
M1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E
M>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@
M("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B
M='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E
M9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@
M("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S
M:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@
M("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B:&ED92(Z(&9A
M;'-E+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@
M("`@("`@("`B<75E<GDB.B`B0V]N=&%I;F5R1G-7<FET97-">71E<U1O=&%L
M7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#
M;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT;W-T
M<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R:6YG
M*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H3&%B
M96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@3F%M97-P86-E(#T](%PB)$YA;65S
M<&%C95PB7&Y\('=H97)E(%!O9"`]/2!<(B10;V1<(EQN?"!I;G9O:V4@<')O
M;5]D96QT82@I7&Y\(&5X=&5N9"!686QU93U686QU92\V,%QN?"!S=6UM87)I
M>F4@5F%L=64]879G*%9A;'5E*2!B>2!B:6XH5&EM97-T86UP+"`D7U]T:6UE
M26YT97)V86PI+"!T;W-T<FEN9RA,86)E;',N:60I+"!0;V1<;GP@<')O:F5C
M="!4:6UE<W1A;7`L(%=R:71E/59A;'5E7&Y\(&]R9&5R(&)Y(%1I;65S=&%M
M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@
M("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-
M;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@("`@("`@
M("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@("`@?0T*
M("`@("`@72P-"B`@("`@(")T:71L92(Z(")$:7-K(%1H<F]U9VAP=70B+`T*
M("`@("`@(G1Y<&4B.B`B=&EM97-E<FEE<R(-"B`@("!]+`T*("`@('L-"B`@
M("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA
M+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U
M:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@?2P-"B`@("`@(")F
M:65L9$-O;F9I9R(Z('L-"B`@("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@
M("`@(")C;VQO<B(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@(G!A;&5T=&4M
M8VQA<W-I8R(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")C=7-T;VTB.B![
M#0H@("`@("`@("`@("`B87AI<T)O<F1E<E-H;W<B.B!F86QS92P-"B`@("`@
M("`@("`@(")A>&ES0V5N=&5R961:97)O(CH@9F%L<V4L#0H@("`@("`@("`@
M("`B87AI<T-O;&]R36]D92(Z(")T97AT(BP-"B`@("`@("`@("`@(")A>&ES
M3&%B96PB.B`B(BP-"B`@("`@("`@("`@(")A>&ES4&QA8V5M96YT(CH@(F%U
M=&\B+`T*("`@("`@("`@("`@(F)A<D%L:6=N;65N="(Z(#`L#0H@("`@("`@
M("`@("`B9')A=U-T>6QE(CH@(FQI;F4B+`T*("`@("`@("`@("`@(F9I;&Q/
M<&%C:71Y(CH@,3`L#0H@("`@("`@("`@("`B9W)A9&EE;G1-;V1E(CH@(FYO
M;F4B+`T*("`@("`@("`@("`@(FAI9&5&<F]M(CH@>PT*("`@("`@("`@("`@
M("`B;&5G96YD(CH@9F%L<V4L#0H@("`@("`@("`@("`@(")T;V]L=&EP(CH@
M9F%L<V4L#0H@("`@("`@("`@("`@(")V:7HB.B!F86QS90T*("`@("`@("`@
M("`@?2P-"B`@("`@("`@("`@(")I;G-E<G1.=6QL<R(Z(&9A;'-E+`T*("`@
M("`@("`@("`@(FQI;F5);G1E<G!O;&%T:6]N(CH@(FQI;F5A<B(L#0H@("`@
M("`@("`@("`B;&EN95=I9'1H(CH@,2P-"B`@("`@("`@("`@(")P;VEN=%-I
M>F4B.B`U+`T*("`@("`@("`@("`@(G-C86QE1&ES=')I8G5T:6]N(CH@>PT*
M("`@("`@("`@("`@("`B='EP92(Z(")L:6YE87(B#0H@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@(G-H;W=0;VEN=',B.B`B;F5V97(B+`T*("`@("`@
M("`@("`@(G-P86Y.=6QL<R(Z(&9A;'-E+`T*("`@("`@("`@("`@(G-T86-K
M:6YG(CH@>PT*("`@("`@("`@("`@("`B9W)O=7`B.B`B02(L#0H@("`@("`@
M("`@("`@(")M;V1E(CH@(FYO;F4B#0H@("`@("`@("`@("!]+`T*("`@("`@
M("`@("`@(G1H<F5S:&]L9'-3='EL92(Z('L-"B`@("`@("`@("`@("`@(FUO
M9&4B.B`B;V9F(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@
M("`@("`@(FUA<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B
M.B![#0H@("`@("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@
M("`@("`B<W1E<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@
M("`@("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU
M92(Z(&YU;&P-"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@>PT*
M("`@("`@("`@("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@("`@
M("`@(")V86QU92(Z(#@P#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@
M(%T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")U;FET(CH@(FEO<',B#0H@
M("`@("`@('TL#0H@("`@("`@(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-
M"B`@("`@(")G<FED4&]S(CH@>PT*("`@("`@("`B:"(Z(#<L#0H@("`@("`@
M(")W(CH@.2P-"B`@("`@("`@(G@B.B`Q,"P-"B`@("`@("`@(GDB.B`T.`T*
M("`@("`@?2P-"B`@("`@(")I9"(Z(#$T+`T*("`@("`@(FEN=&5R=F%L(CH@
M(C%M(BP-"B`@("`@(")O<'1I;VYS(CH@>PT*("`@("`@("`B;&5G96YD(CH@
M>PT*("`@("`@("`@(")C86QC<R(Z(%M=+`T*("`@("`@("`@(")D:7-P;&%Y
M36]D92(Z(")L:7-T(BP-"B`@("`@("`@("`B<&QA8V5M96YT(CH@(F)O='1O
M;2(L#0H@("`@("`@("`@(G-H;W=,96=E;F0B.B!T<G5E#0H@("`@("`@('TL
M#0H@("`@("`@(")T;V]L=&EP(CH@>PT*("`@("`@("`@(")M;V1E(CH@(G-I
M;F=L92(L#0H@("`@("`@("`@(G-O<G0B.B`B;F]N92(-"B`@("`@("`@?0T*
M("`@("`@?2P-"B`@("`@(")T87)G971S(CH@6PT*("`@("`@("![#0H@("`@
M("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z
M(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@
M("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD
M871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E
M.G1E>'1](@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB
M.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@
M(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N
M9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*
M("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@
M("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@
M(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-
M"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N
M,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B0V]N=&%I;F5R1G-296%D<U1O
M=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R
M92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT
M;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R
M:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H
M3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@3F%M97-P86-E(#T](%PB)$YA
M;65S<&%C95PB7&Y\('=H97)E(%!O9"`]/2!<(B10;V1<(EQN?"!I;G9O:V4@
M<')O;5]D96QT82@I7&Y\(&5X=&5N9"!686QU93U686QU92\V,%QN?"!S=6UM
M87)I>F4@5F%L=64]879G*%9A;'5E*2HM,2!B>2!B:6XH5&EM97-T86UP+"`D
M7U]T:6UE26YT97)V86PI+"!T;W-T<FEN9RA,86)E;',N:60I+"!0;V1<;GP@
M<')O:F5C="!4:6UE<W1A;7`L(%)E860]5F%L=65<;GP@;W)D97(@8GD@5&EM
M97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W
M(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@
M(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")296-E:79E
M($)Y=&5S(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?<V5R
M:65S(@T*("`@("`@("!]+`T*("`@("`@("![#0H@("`@("`@("`@(D]P96Y!
M22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-
M"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E
M(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-
M"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@
M("`@("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@
M("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS
M(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@
M("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@
M("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B
M86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-
M"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@
M("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]
M+`T*("`@("`@("`@(")H:61E(CH@9F%L<V4L#0H@("`@("`@("`@(G!L=6=I
M;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R>2(Z(")#;VYT
M86EN97)&<U=R:71E<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM
M97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT
M96YD($YA;65S<&%C93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E
M>'1E;F0@4&]D/71O<W1R:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT
M86EN97(]=&]S=')I;F<H3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@3F%M
M97-P86-E(#T](%PB)$YA;65S<&%C95PB7&Y\('=H97)E(%!O9"`]/2!<(B10
M;V1<(EQN?"!I;G9O:V4@<')O;5]D96QT82@I7&Y\(&5X=&5N9"!686QU93U6
M86QU92\V,%QN?"!S=6UM87)I>F4@5F%L=64]879G*%9A;'5E*2!B>2!B:6XH
M5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI+"!T;W-T<FEN9RA,86)E;',N
M:60I+"!0;V1<;GP@<')O:F5C="!4:6UE<W1A;7`L(%1R86YS;6ET/59A;'5E
M7&Y\(&]R9&5R(&)Y(%1I;65S=&%M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U
M97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+
M44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B
M<F5F260B.B`B02(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE
M7W-E<FEE<R(-"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z
M(")$:7-K($E/4%,B+`T*("`@("`@(G1Y<&4B.B`B=&EM97-E<FEE<R(-"B`@
M("!]#0H@(%TL#0H@(")R969R97-H(CH@(B(L#0H@(")S8VAE;6%697)S:6]N
M(CH@,SDL#0H@(")T86=S(CH@6UTL#0H@(")T96UP;&%T:6YG(CH@>PT*("`@
M(")L:7-T(CH@6PT*("`@("`@>PT*("`@("`@("`B:&ED92(Z(#`L#0H@("`@
M("`@(")I;F-L=61E06QL(CH@9F%L<V4L#0H@("`@("`@(")M=6QT:2(Z(&9A
M;'-E+`T*("`@("`@("`B;F%M92(Z(")$871A<V]U<F-E(BP-"B`@("`@("`@
M(F]P=&EO;G,B.B!;72P-"B`@("`@("`@(G%U97)Y(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G%U97)Y
M5F%L=64B.B`B(BP-"B`@("`@("`@(G)E9G)E<V@B.B`Q+`T*("`@("`@("`B
M<F5G97@B.B`B(BP-"B`@("`@("`@(G-K:7!5<FQ3>6YC(CH@9F%L<V4L#0H@
M("`@("`@(")T>7!E(CH@(F1A=&%S;W5R8V4B#0H@("`@("!]+`T*("`@("`@
M>PT*("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`B='EP92(Z
M(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@
M("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@
M('TL#0H@("`@("`@(")D969I;FET:6]N(CH@(D-O;G1A:6YE<D-P=55S86=E
M4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP
M*5QN?"!D:7-T:6YC="!#;'5S=&5R7&Y\(&]R9&5R(&)Y($-L=7-T97(@87-C
M(BP-"B`@("`@("`@(FAI9&4B.B`P+`T*("`@("`@("`B:6YC;'5D94%L;"(Z
M(&9A;'-E+`T*("`@("`@("`B;75L=&DB.B!F86QS92P-"B`@("`@("`@(FYA
M;64B.B`B0VQU<W1E<B(L#0H@("`@("`@(")O<'1I;VYS(CH@6UTL#0H@("`@
M("`@(")Q=65R>2(Z('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@
M("`@("`@("`@(F%Z=7)E3&]G06YA;'ET:6-S(CH@>PT*("`@("`@("`@("`@
M(G%U97)Y(CH@(B(L#0H@("`@("`@("`@("`B<F5S;W5R8V5S(CH@6UT-"B`@
M("`@("`@("!]+`T*("`@("`@("`@(")C;'5S=&5R57)I(CH@(B(L#0H@("`@
M("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")E>'!R
M97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@
M("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP
M92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C
M92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@
M("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@
M("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N
M<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@
M("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO
M;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<D-P
M=55S86=E4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM
M97-T86UP*5QN?"!D:7-T:6YC="!#;'5S=&5R7&Y\(&]R9&5R(&)Y($-L=7-T
M97(@87-C(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@
M("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO
M9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")!(BP-"B`@("`@("`@
M("`B<F5S=6QT1F]R;6%T(CH@(G1A8FQE(BP-"B`@("`@("`@("`B<W5B<V-R
M:7!T:6]N(CH@(C)&-#5&-4(P+3=%13(M-$4Y,"U!-#$V+40S.#@Q0D1#1C<X
M,R(-"B`@("`@("`@?2P-"B`@("`@("`@(G)E9G)E<V@B.B`R+`T*("`@("`@
M("`B<F5G97@B.B`B(BP-"B`@("`@("`@(G-K:7!5<FQ3>6YC(CH@9F%L<V4L
M#0H@("`@("`@(")S;W)T(CH@,"P-"B`@("`@("`@(G1Y<&4B.B`B<75E<GDB
M#0H@("`@("!]+`T*("`@("`@>PT*("`@("`@("`B8W5R<F5N="(Z('L-"B`@
M("`@("`@("`B<V5L96-T960B.B!F86QS92P-"B`@("`@("`@("`B=&5X="(Z
M(")A9'@M;6]N(BP-"B`@("`@("`@("`B=F%L=64B.B`B861X+6UO;B(-"B`@
M("`@("`@?2P-"B`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@
M(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R
M8V4B+`T*("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*
M("`@("`@("!]+`T*("`@("`@("`B9&5F:6YI=&EO;B(Z(")#;VYT86EN97)#
M<'55<V5R4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM
M97-T86UP*5QN?"!W:&5R92!#;'5S=&5R(#T](%PB)$-L=7-T97)<(EQN?"!W
M:&5R92!,86)E;',N;F%M97-P86-E("$](%PB7")<;GP@9&ES=&EN8W0@=&]S
M=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@;W)D97(@8GD@3&%B96QS7VYA
M;65S<&%C92!A<V,B+`T*("`@("`@("`B:&ED92(Z(#`L#0H@("`@("`@(")I
M;F-L=61E06QL(CH@9F%L<V4L#0H@("`@("`@(")M=6QT:2(Z(&9A;'-E+`T*
M("`@("`@("`B;F%M92(Z(").86UE<W!A8V4B+`T*("`@("`@("`B;W!T:6]N
M<R(Z(%M=+`T*("`@("`@("`B<75E<GDB.B![#0H@("`@("`@("`@(D]P96Y!
M22(Z(&9A;'-E+`T*("`@("`@("`@(")A>G5R94QO9T%N86QY=&EC<R(Z('L-
M"B`@("`@("`@("`@(")Q=65R>2(Z("(B+`T*("`@("`@("`@("`@(G)E<V]U
M<F-E<R(Z(%M=#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B8VQU<W1E<E5R
M:2(Z("(B+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@
M("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")F<F]M(CH@
M>PT*("`@("`@("`@("`@("`B<')O<&5R='DB.B![#0H@("`@("`@("`@("`@
M("`@(FYA;64B.B`B0V]N=&%I;F5R0W!U57-E<E-E8V]N9'-4;W1A;"(L#0H@
M("`@("`@("`@("`@("`@(G1Y<&4B.B`B<W1R:6YG(@T*("`@("`@("`@("`@
M("!]+`T*("`@("`@("`@("`@("`B='EP92(Z(")P<F]P97)T>2(-"B`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@
M("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E
M(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E
M(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@
M("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS
M(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@
M("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N
M(CH@(C4N,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B0V]N=&%I;F5R0W!U
M57-E<E-E8V]N9'-4;W1A;%QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S
M=&%M<"E<;GP@=VAE<F4@0VQU<W1E<B`]/2!<(B1#;'5S=&5R7")<;GP@=VAE
M<F4@3&%B96QS+FYA;65S<&%C92`A/2!<(EPB7&Y\(&1I<W1I;F-T('1O<W1R
M:6YG*$QA8F5L<RYN86UE<W!A8V4I7&Y\(&]R9&5R(&)Y($QA8F5L<U]N86UE
M<W!A8V4@87-C(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-
M"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A
M=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")!(BP-"B`@("`@
M("`@("`B<F5S=6QT1F]R;6%T(CH@(G1A8FQE(BP-"B`@("`@("`@("`B<W5B
M<V-R:7!T:6]N(CH@(C)&-#5&-4(P+3=%13(M-$4Y,"U!-#$V+40S.#@Q0D1#
M1C<X,R(-"B`@("`@("`@?2P-"B`@("`@("`@(G)E9G)E<V@B.B`R+`T*("`@
M("`@("`B<F5G97@B.B`B(BP-"B`@("`@("`@(G-K:7!5<FQ3>6YC(CH@9F%L
M<V4L#0H@("`@("`@(")S;W)T(CH@,"P-"B`@("`@("`@(G1Y<&4B.B`B<75E
M<GDB#0H@("`@("!]+`T*("`@("`@>PT*("`@("`@("`B9&%T87-O=7)C92(Z
M('L-"B`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP
M;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S
M;W5R8V4Z=&5X='TB#0H@("`@("`@('TL#0H@("`@("`@(")D969I;FET:6]N
M(CH@(D-O;G1A:6YE<D-P=55S86=E4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?
M7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R(#T](%PB
M)$-L=7-T97)<(EQN?"!W:&5R92!,86)E;',N;F%M97-P86-E(#T](%PB)$YA
M;65S<&%C95PB7&Y\('=H97)E($QA8F5L<RYP;V0@(3T@7")<(EQN?"!D:7-T
M:6YC="!T;W-T<FEN9RA,86)E;',N<&]D*5QN?"!O<F1E<B!B>2!,86)E;'-?
M<&]D(&%S8R(L#0H@("`@("`@(")H:61E(CH@,"P-"B`@("`@("`@(FEN8VQU
M9&5!;&PB.B!F86QS92P-"B`@("`@("`@(FUU;'1I(CH@9F%L<V4L#0H@("`@
M("`@(")N86UE(CH@(E!O9"(L#0H@("`@("`@(")O<'1I;VYS(CH@6UTL#0H@
M("`@("`@(")Q=65R>2(Z('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L
M#0H@("`@("`@("`@(F%Z=7)E3&]G06YA;'ET:6-S(CH@>PT*("`@("`@("`@
M("`@(G%U97)Y(CH@(B(L#0H@("`@("`@("`@("`B<F5S;W5R8V5S(CH@6UT-
M"B`@("`@("`@("!]+`T*("`@("`@("`@(")C;'5S=&5R57)I(CH@(B(L#0H@
M("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")E
M>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@
M("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B
M='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E
M9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@
M("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S
M:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@
M("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R
M<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE
M<D-P=55S86=E4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H
M5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R(#T](%PB)$-L=7-T97)<(EQN
M?"!W:&5R92!,86)E;',N;F%M97-P86-E(#T](%PB)$YA;65S<&%C95PB7&Y\
M('=H97)E($QA8F5L<RYP;V0@(3T@7")<(EQN?"!D:7-T:6YC="!T;W-T<FEN
M9RA,86)E;',N<&]D*5QN?"!O<F1E<B!B>2!,86)E;'-?<&]D(&%S8R(L#0H@
M("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U
M97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-
M"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@("`@("`@("`@(G)E<W5L=$9O
M<FUA="(Z(")T86)L92(L#0H@("`@("`@("`@(G-U8G-C<FEP=&EO;B(Z("(R
M1C0U1C5","TW144R+31%.3`M030Q-BU$,S@X,4)$0T8W.#,B#0H@("`@("`@
M('TL#0H@("`@("`@(")R969R97-H(CH@,BP-"B`@("`@("`@(G)E9V5X(CH@
M(B(L#0H@("`@("`@(")S:VEP57)L4WEN8R(Z(&9A;'-E+`T*("`@("`@("`B
M<V]R="(Z(#`L#0H@("`@("`@(")T>7!E(CH@(G%U97)Y(@T*("`@("`@?0T*
M("`@(%T-"B`@?2P-"B`@(G1I;64B.B![#0H@("`@(F9R;VTB.B`B;F]W+3%H
M(BP-"B`@("`B=&\B.B`B;F]W(@T*("!]+`T*("`B=&EM97!I8VME<B(Z('L-
M"B`@("`B;F]W1&5L87DB.B`B(@T*("!]+`T*("`B=&EM97IO;F4B.B`B=71C
L(BP-"B`@(G1I=&QE(CH@(E!O9',B+`T*("`B=V5E:U-T87)T(CH@(B(-"GUC
`
end
SHAR_EOF
  (set 20 24 12 06 20 59 58 'dashboards/pods.json'
   eval "${shar_touch}") && \
  chmod 0644 'dashboards/pods.json'
if test $? -ne 0
then ${echo} "restore of dashboards/pods.json failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'dashboards/pods.json': 'MD5 check failed'
       ) << \SHAR_EOF
ca9c83c18c78adf24b07c1f318fc89aa  dashboards/pods.json
SHAR_EOF

else
test `LC_ALL=C wc -c < 'dashboards/pods.json'` -ne 65204 && \
  ${echo} "restoration warning:  size of 'dashboards/pods.json' is not 65204"
  fi
# ============= dashboards/api-server.json ==============
  sed 's/^X//' << 'SHAR_EOF' | uudecode &&
begin 600 dashboards/api-server.json
M>PT*("`B86YN;W1A=&EO;G,B.B![#0H@("`@(FQI<W0B.B!;#0H@("`@("![
M#0H@("`@("`@(")B=6EL=$EN(CH@,2P-"B`@("`@("`@(F1A=&%S;W5R8V4B
M.B![#0H@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82(L#0H@("`@("`@("`@
M(G5I9"(Z("(M+2!'<F%F86YA("TM(@T*("`@("`@("!]+`T*("`@("`@("`B
M96YA8FQE(CH@=')U92P-"B`@("`@("`@(FAI9&4B.B!T<G5E+`T*("`@("`@
M("`B:6-O;D-O;&]R(CH@(G)G8F$H,"P@,C$Q+"`R-34L(#$I(BP-"B`@("`@
M("`@(FYA;64B.B`B06YN;W1A=&EO;G,@)B!!;&5R=',B+`T*("`@("`@("`B
M='EP92(Z(")D87-H8F]A<F0B#0H@("`@("!]#0H@("`@70T*("!]+`T*("`B
M961I=&%B;&4B.B!T<G5E+`T*("`B9FES8V%L665A<E-T87)T36]N=&@B.B`P
M+`T*("`B9W)A<&A4;V]L=&EP(CH@,"P-"B`@(FED(CH@-#@L#0H@(")L:6YK
M<R(Z(%L-"B`@("![#0H@("`@("`B87-$<F]P9&]W;B(Z(&9A;'-E+`T*("`@
M("`@(FEC;VXB.B`B97AT97)N86P@;&EN:R(L#0H@("`@("`B:6YC;'5D959A
M<G,B.B!F86QS92P-"B`@("`@(")K965P5&EM92(Z(&9A;'-E+`T*("`@("`@
M(G1A9W,B.B!;72P-"B`@("`@(")T87)G971";&%N:R(Z('1R=64L#0H@("`@
M("`B=&ET;&4B.B`B4V5A<F-H($QO9W,B+`T*("`@("`@(G1O;VQT:7`B.B`B
M(BP-"B`@("`@(")T>7!E(CH@(FQI;FLB+`T*("`@("`@(G5R;"(Z("(D3&]G
M<U523"(-"B`@("!]#0H@(%TL#0H@(")P86YE;',B.B!;#0H@("`@>PT*("`@
M("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A;F$M
M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G5I
M9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("!]+`T*("`@("`@(F9I
M96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@("`@("`@
M("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B<&%L971T92UC
M;&%S<VEC(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F-U<W1O;2(Z('L-
M"B`@("`@("`@("`@(")A>&ES0F]R9&5R4VAO=R(Z(&9A;'-E+`T*("`@("`@
M("`@("`@(F%X:7-#96YT97)E9%IE<F\B.B!F86QS92P-"B`@("`@("`@("`@
M(")A>&ES0V]L;W)-;V1E(CH@(G1E>'0B+`T*("`@("`@("`@("`@(F%X:7-,
M86)E;"(Z("(B+`T*("`@("`@("`@("`@(F%X:7-0;&%C96UE;G0B.B`B875T
M;R(L#0H@("`@("`@("`@("`B8F%R06QI9VYM96YT(CH@,"P-"B`@("`@("`@
M("`@(")D<F%W4W1Y;&4B.B`B;&EN92(L#0H@("`@("`@("`@("`B9FEL;$]P
M86-I='DB.B`P+`T*("`@("`@("`@("`@(F=R861I96YT36]D92(Z(")N;VYE
M(BP-"B`@("`@("`@("`@(")H:61E1G)O;2(Z('L-"B`@("`@("`@("`@("`@
M(FQE9V5N9"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=&]O;'1I<"(Z(&9A
M;'-E+`T*("`@("`@("`@("`@("`B=FEZ(CH@9F%L<V4-"B`@("`@("`@("`@
M('TL#0H@("`@("`@("`@("`B:6YS97)T3G5L;',B.B!F86QS92P-"B`@("`@
M("`@("`@(")L:6YE26YT97)P;VQA=&EO;B(Z(")L:6YE87(B+`T*("`@("`@
M("`@("`@(FQI;F57:61T:"(Z(#$L#0H@("`@("`@("`@("`B<&]I;G13:7IE
M(CH@-2P-"B`@("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z('L-"B`@
M("`@("`@("`@("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")S:&]W4&]I;G1S(CH@(F%U=&\B+`T*("`@("`@("`@
M("`@(G-P86Y.=6QL<R(Z(&9A;'-E+`T*("`@("`@("`@("`@(G-T86-K:6YG
M(CH@>PT*("`@("`@("`@("`@("`B9W)O=7`B.B`B02(L#0H@("`@("`@("`@
M("`@(")M;V1E(CH@(FYO;F4B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@
M("`@(G1H<F5S:&]L9'-3='EL92(Z('L-"B`@("`@("`@("`@("`@(FUO9&4B
M.B`B;V9F(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@
M("`@(FUA<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![
M#0H@("`@("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@
M("`B<W1E<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@
M("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU92(Z
M(&YU;&P-"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@>PT*("`@
M("`@("`@("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@("`@("`@
M(")V86QU92(Z(#@P#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-
M"B`@("`@("`@("!]+`T*("`@("`@("`@(")U;FET(CH@(G)E<7!S(@T*("`@
M("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6PT*("`@("`@("`@('L-
M"B`@("`@("`@("`@(")M871C:&5R(CH@>PT*("`@("`@("`@("`@("`B:60B
M.B`B8GE.86UE(BP-"B`@("`@("`@("`@("`@(F]P=&EO;G,B.B`B5F%L=64B
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G!R;W!E<G1I97,B.B!;
M#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B:60B.B`B8V]L
M;W(B+`T*("`@("`@("`@("`@("`@(")V86QU92(Z('L-"B`@("`@("`@("`@
M("`@("`@(")F:7AE9$-O;&]R(CH@(G)E9"(L#0H@("`@("`@("`@("`@("`@
M("`B;6]D92(Z(")F:7AE9"(-"B`@("`@("`@("`@("`@("!]#0H@("`@("`@
M("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@("`@("!]+`T*("`@("`@
M("`@('L-"B`@("`@("`@("`@(")M871C:&5R(CH@>PT*("`@("`@("`@("`@
M("`B:60B.B`B8GE.86UE(BP-"B`@("`@("`@("`@("`@(F]P=&EO;G,B.B`B
M17)R;W)S(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")P<F]P97)T
M:65S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(FED
M(CH@(F-O;&]R(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B![#0H@("`@
M("`@("`@("`@("`@("`B9FEX961#;VQO<B(Z(")R960B+`T*("`@("`@("`@
M("`@("`@("`@(FUO9&4B.B`B9FEX960B#0H@("`@("`@("`@("`@("`@?0T*
M("`@("`@("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?0T*
M("`@("`@("!=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@
M("`@(")H(CH@."P-"B`@("`@("`@(G<B.B`X+`T*("`@("`@("`B>"(Z(#`L
M#0H@("`@("`@(")Y(CH@,`T*("`@("`@?2P-"B`@("`@(")I9"(Z(#$V.2P-
M"B`@("`@(")I;G1E<G9A;"(Z("(Q;2(L#0H@("`@("`B;W!T:6]N<R(Z('L-
M"B`@("`@("`@(FQE9V5N9"(Z('L-"B`@("`@("`@("`B8V%L8W,B.B!;72P-
M"B`@("`@("`@("`B9&ES<&QA>4UO9&4B.B`B;&ES="(L#0H@("`@("`@("`@
M(G!L86-E;65N="(Z(")B;W1T;VTB+`T*("`@("`@("`@(")S:&]W3&5G96YD
M(CH@=')U90T*("`@("`@("!]+`T*("`@("`@("`B=&]O;'1I<"(Z('L-"B`@
M("`@("`@("`B;6]D92(Z(")S:6YG;&4B+`T*("`@("`@("`@(")S;W)T(CH@
M(FYO;F4B#0H@("`@("`@('T-"B`@("`@('TL#0H@("`@("`B=&%R9V5T<R(Z
M(%L-"B`@("`@("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@
M("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A
M=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z
M=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B
M=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@
M("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB
M.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@
M("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@
M("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS
M(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@
M("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@
M(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A
M;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B
M<&QU9VEN5F5R<VEO;B(Z("(U+C`N-2(L#0H@("`@("`@("`@(G%U97)Y(CH@
M(D%P:7-E<G9E<E)E<75E<W14;W1A;%QN?"!W:&5R92`D7U]T:6UE1FEL=&5R
M*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<B`]/2!<(B1#;'5S=&5R7")<
M;GP@:6YV;VME('!R;VU?<F%T92@I7&Y\('-U;6UA<FEZ92!686QU93US=6TH
M5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"E<;GP@
M<')O:F5C="!4:6UE<W1A;7`L(%)E<75E<W1S/59A;'5E7&Y\(&]R9&5R(&)Y
M(%1I;65S=&%M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@
M(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@
M("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B02(L
M#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@
M("`@("`@?2P-"B`@("`@("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS
M92P-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@
M("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F
M86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@
M("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]
M+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R
M;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*
M("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*
M("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E
M<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@
M("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@
M("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP
M92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@
M("`@("`B:&ED92(Z(&9A;'-E+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N
M(CH@(C4N,"XU(BP-"B`@("`@("`@("`B<75E<GDB.B`B07!I<V5R=F5R4F5Q
M=65S=%1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN
M?"!W:&5R92!#;'5S=&5R(#T](%PB)$-L=7-T97)<(EQN?"!W:&5R92!,86)E
M;',N8V]D92!S=&%R='-W:71H(%PB-5PB7&Y\(&EN=F]K92!P<F]M7W)A=&4H
M*5QN?"!S=6UM87)I>F4@5F%L=64]<W5M*%9A;'5E*2!B>2!B:6XH5&EM97-T
M86UP+"`D7U]T:6UE26YT97)V86PI7&Y\('!R;VIE8W0@5&EM97-T86UP+"!%
M<G)O<G,]5F%L=65<;GP@;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@
M("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E
M<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*
M("`@("`@("`@(")R969)9"(Z(")"(BP-"B`@("`@("`@("`B<F5S=6QT1F]R
M;6%T(CH@(G1I;65?<V5R:65S(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@
M("`@(G1I=&QE(CH@(E1H<F]U9VAP=70B+`T*("`@("`@(G1Y<&4B.B`B=&EM
M97-E<FEE<R(-"B`@("!]+`T*("`@('L-"B`@("`@(")D871A<V]U<F-E(CH@
M>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R
M97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E
M.G1E>'1](@T*("`@("`@?2P-"B`@("`@(")F:65L9$-O;F9I9R(Z('L-"B`@
M("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@(")C=7-T;VTB.B![#0H@
M("`@("`@("`@("`B:&ED949R;VTB.B![#0H@("`@("`@("`@("`@(")L96=E
M;F0B.B!F86QS92P-"B`@("`@("`@("`@("`@(G1O;VQT:7`B.B!F86QS92P-
M"B`@("`@("`@("`@("`@(G9I>B(Z(&9A;'-E#0H@("`@("`@("`@("!]+`T*
M("`@("`@("`@("`@(G-C86QE1&ES=')I8G5T:6]N(CH@>PT*("`@("`@("`@
M("`@("`B='EP92(Z(")L:6YE87(B#0H@("`@("`@("`@("!]#0H@("`@("`@
M("`@?2P-"B`@("`@("`@("`B9FEE;&1-:6Y-87@B.B!F86QS90T*("`@("`@
M("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6UT-"B`@("`@('TL#0H@("`@
M("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`X+`T*("`@("`@("`B=R(Z
M(#@L#0H@("`@("`@(")X(CH@."P-"B`@("`@("`@(GDB.B`P#0H@("`@("!]
M+`T*("`@("`@(FED(CH@,S`X+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@
M("`@(")C86QC=6QA=&4B.B!F86QS92P-"B`@("`@("`@(F-E;&Q'87`B.B`P
M+`T*("`@("`@("`B8V]L;W(B.B![#0H@("`@("`@("`@(F5X<&]N96YT(CH@
M,"XU+`T*("`@("`@("`@(")F:6QL(CH@(F1A<FLM;W)A;F=E(BP-"B`@("`@
M("`@("`B;6]D92(Z(")S8VAE;64B+`T*("`@("`@("`@(")R979E<G-E(CH@
M9F%L<V4L#0H@("`@("`@("`@(G-C86QE(CH@(F5X<&]N96YT:6%L(BP-"B`@
M("`@("`@("`B<V-H96UE(CH@(D]R86YG97,B+`T*("`@("`@("`@(")S=&5P
M<R(Z(#,R#0H@("`@("`@('TL#0H@("`@("`@(")E>&5M<&QA<G,B.B![#0H@
M("`@("`@("`@(F-O;&]R(CH@(G)G8F$H,C4U+#`L,C4U+#`N-RDB#0H@("`@
M("`@('TL#0H@("`@("`@(")F:6QT97)686QU97,B.B![#0H@("`@("`@("`@
M(FQE(CH@,64M.0T*("`@("`@("!]+`T*("`@("`@("`B;&5G96YD(CH@>PT*
M("`@("`@("`@(")S:&]W(CH@=')U90T*("`@("`@("!]+`T*("`@("`@("`B
M<F]W<T9R86UE(CH@>PT*("`@("`@("`@(")L87EO=70B.B`B875T;R(-"B`@
M("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@("`@(FUO
M9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<VAO=T-O;&]R4V-A;&4B.B!F
M86QS92P-"B`@("`@("`@("`B>4AI<W1O9W)A;2(Z(&9A;'-E#0H@("`@("`@
M('TL#0H@("`@("`@(")Y07AI<R(Z('L-"B`@("`@("`@("`B87AI<U!L86-E
M;65N="(Z(")L969T(BP-"B`@("`@("`@("`B<F5V97)S92(Z(&9A;'-E#0H@
M("`@("`@('T-"B`@("`@('TL#0H@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(Q
M,"XT+C$Q(BP-"B`@("`@(")T87)G971S(CH@6PT*("`@("`@("![#0H@("`@
M("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z
M(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@
M("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD
M871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E
M.G1E>'1](@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB
M.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@
M(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N
M9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*
M("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@
M("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@
M(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-
M"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N
M,"XU(BP-"B`@("`@("`@("`B<75E<GDB.B`B07!I<V5R=F5R4F5Q=65S=$1U
M<F%T:6]N4V5C;VYD<T)U8VME=%QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I
M;65S=&%M<"E<;GP@97AT96YD(%-E<FEE<SUT;W)E86PH3&%B96QS+FQE*5QN
M?"!W:&5R92!,86)E;',N;&4@(3T@7"(K26YF7")<;GP@:6YV;VME('!R;VU?
M9&5L=&$H*5QN?"!S=6UM87)I>F4@5F%L=64]<W5M*%9A;'5E*2!B>2!B:6XH
M5&EM97-T86UP+"`Q;2DL(%-E<FEE<UQN?"!P<F]J96-T(%1I;65S=&%M<"P@
M4V5R:65S+"!686QU95QN?"!O<F1E<B!B>2!4:6UE<W1A;7`@9&5S8RP@4V5R
M:65S(&%S8UQN?"!E>'1E;F0@5F%L=64]8V%S92AP<F5V*%-E<FEE<RD@/"!3
M97)I97,L(&EF9BA686QU92UP<F5V*%9A;'5E*2`^(#`L(%9A;'5E+7!R978H
M5F%L=64I+"!T;W)E86PH,"DI+"!686QU92E<;GP@<')O:F5C="!4:6UE<W1A
M;7`L('1O<W1R:6YG*%-E<FEE<RDL(%9A;'5E7&Y\(&]R9&5R(&)Y(%1I;65S
M=&%M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L
M#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R
M87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@("`@
M("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@("`@
M?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z("),871E;F-Y(BP-"B`@("`@
M(")T>7!E(CH@(FAE871M87`B#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T
M87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD
M871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[
M1&%T87-O=7)C93IT97AT?2(-"B`@("`@('TL#0H@("`@("`B9FEE;&1#;VYF
M:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z('L-"B`@("`@("`@("`B8V]L
M;W(B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")T:')E<VAO;&1S(@T*("`@
M("`@("`@('TL#0H@("`@("`@("`@(FUA<'!I;F=S(CH@6UTL#0H@("`@("`@
M("`@(G1H<F5S:&]L9',B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")A8G-O
M;'5T92(L#0H@("`@("`@("`@("`B<W1E<',B.B!;#0H@("`@("`@("`@("`@
M('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@
M("`@("`@("`@(")V86QU92(Z(&YU;&P-"B`@("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")R960B
M+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(#@P#0H@("`@("`@("`@("`@
M('T-"B`@("`@("`@("`@(%T-"B`@("`@("`@("!]#0H@("`@("`@('TL#0H@
M("`@("`@(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED
M4&]S(CH@>PT*("`@("`@("`B:"(Z(#@L#0H@("`@("`@(")W(CH@."P-"B`@
M("`@("`@(G@B.B`Q-BP-"B`@("`@("`@(GDB.B`P#0H@("`@("!]+`T*("`@
M("`@(FED(CH@,RP-"B`@("`@(")I;G1E<G9A;"(Z("(Q;2(L#0H@("`@("`B
M;W!T:6]N<R(Z('L-"B`@("`@("`@(F1I<W!L87E-;V1E(CH@(F=R861I96YT
M(BP-"B`@("`@("`@(FUA>%9I>DAE:6=H="(Z(#,P,"P-"B`@("`@("`@(FUI
M;E9I>DAE:6=H="(Z(#$V+`T*("`@("`@("`B;6EN5FEZ5VED=&@B.B`X+`T*
M("`@("`@("`B;F%M95!L86-E;65N="(Z(")A=71O(BP-"B`@("`@("`@(F]R
M:65N=&%T:6]N(CH@(FAO<FEZ;VYT86PB+`T*("`@("`@("`B<F5D=6-E3W!T
M:6]N<R(Z('L-"B`@("`@("`@("`B8V%L8W,B.B!;#0H@("`@("`@("`@("`B
M;&%S=$YO=$YU;&PB#0H@("`@("`@("`@72P-"B`@("`@("`@("`B9FEE;&1S
M(CH@(B]>5F%L=64D+R(L#0H@("`@("`@("`@(G9A;'5E<R(Z('1R=64-"B`@
M("`@("`@?2P-"B`@("`@("`@(G-H;W=5;F9I;&QE9"(Z('1R=64L#0H@("`@
M("`@(")S:7II;F<B.B`B875T;R(L#0H@("`@("`@(")V86QU94UO9&4B.B`B
M:&ED9&5N(@T*("`@("`@?2P-"B`@("`@(")P;'5G:6Y697)S:6]N(CH@(C$P
M+C0N,3$B+`T*("`@("`@(G1A<F=E=',B.B!;#0H@("`@("`@('L-"B`@("`@
M("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F1A=&%B87-E(CH@
M(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@
M("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A
M=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z
M=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z
M('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B
M97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD
M(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![#0H@
M("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@
M("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-
M"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*
M("`@("`@("`@('TL#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-2XP
M+C4B+`T*("`@("`@("`@(")Q=65R>2(Z(")!<&ES97)V97)297%U97-T5&]T
M86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H97)E
M($-L=7-T97(@/3T@7"(D0VQU<W1E<EPB7&Y\('=H97)E($QA8F5L<RYR97-O
M=7)C92`A/2!<(EPB7&Y\(&EN=F]K92!P<F]M7W)A=&4H*5QN?"!E>'1E;F0@
M4V5R:65S/71O<W1R:6YG*$QA8F5L<RYR97-O=7)C92E<;GP@<W5M;6%R:7IE
M(%9A;'5E/7-U;2A686QU92D@8GD@4V5R:65S7&Y\('!R;VIE8W0@4V5R:65S
M+"!686QU95QN?"!O<F1E<B!B>2!686QU92!D97-C7&XB+`T*("`@("`@("`@
M(")Q=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R>51Y<&4B
M.B`B2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@("`@("`@
M("`@(G)E9DED(CH@(D$B+`T*("`@("`@("`@(")R97-U;'1&;W)M870B.B`B
M=&%B;&4B#0H@("`@("`@('T-"B`@("`@(%TL#0H@("`@("`B=&ET;&4B.B`B
M05!)(%-E<G9E<B`M(%)E<75E<W1S($)Y(%)E<V]U<F-E(BP-"B`@("`@(")T
M>7!E(CH@(F)A<F=A=6=E(@T*("`@('TL#0H@("`@>PT*("`@("`@(F1A=&%S
M;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T
M82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z("(D>T1A
M=&%S;W5R8V4Z=&5X='TB#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG
M(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R
M(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B<&%L971T92UC;&%S<VEC(@T*
M("`@("`@("`@('TL#0H@("`@("`@("`@(F-U<W1O;2(Z('L-"B`@("`@("`@
M("`@(")A>&ES0F]R9&5R4VAO=R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X
M:7-#96YT97)E9%IE<F\B.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES0V]L
M;W)-;V1E(CH@(G1E>'0B+`T*("`@("`@("`@("`@(F%X:7-,86)E;"(Z("(B
M+`T*("`@("`@("`@("`@(F%X:7-0;&%C96UE;G0B.B`B875T;R(L#0H@("`@
M("`@("`@("`B8F%R06QI9VYM96YT(CH@,"P-"B`@("`@("`@("`@(")D<F%W
M4W1Y;&4B.B`B;&EN92(L#0H@("`@("`@("`@("`B9FEL;$]P86-I='DB.B`P
M+`T*("`@("`@("`@("`@(F=R861I96YT36]D92(Z(")N;VYE(BP-"B`@("`@
M("`@("`@(")H:61E1G)O;2(Z('L-"B`@("`@("`@("`@("`@(FQE9V5N9"(Z
M(&9A;'-E+`T*("`@("`@("`@("`@("`B=&]O;'1I<"(Z(&9A;'-E+`T*("`@
M("`@("`@("`@("`B=FEZ(CH@9F%L<V4-"B`@("`@("`@("`@('TL#0H@("`@
M("`@("`@("`B:6YS97)T3G5L;',B.B!F86QS92P-"B`@("`@("`@("`@(")L
M:6YE26YT97)P;VQA=&EO;B(Z(")L:6YE87(B+`T*("`@("`@("`@("`@(FQI
M;F57:61T:"(Z(#$L#0H@("`@("`@("`@("`B<&]I;G13:7IE(CH@-2P-"B`@
M("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z('L-"B`@("`@("`@("`@
M("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@
M("`@(")S:&]W4&]I;G1S(CH@(F%U=&\B+`T*("`@("`@("`@("`@(G-P86Y.
M=6QL<R(Z(&9A;'-E+`T*("`@("`@("`@("`@(G-T86-K:6YG(CH@>PT*("`@
M("`@("`@("`@("`B9W)O=7`B.B`B02(L#0H@("`@("`@("`@("`@(")M;V1E
M(CH@(FYO;F4B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G1H<F5S
M:&]L9'-3='EL92(Z('L-"B`@("`@("`@("`@("`@(FUO9&4B.B`B;V9F(@T*
M("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(FUA<'!I
M;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@("`@("`@
M("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B<W1E<',B
M.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B
M.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(&YU;&P-"B`@
M("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@
M("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@("`@("`@(")V86QU92(Z
M(#@P#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@("`@
M("!]#0H@("`@("`@('TL#0H@("`@("`@(")O=F5R<FED97,B.B!;70T*("`@
M("`@?2P-"B`@("`@(")G<FED4&]S(CH@>PT*("`@("`@("`B:"(Z(#@L#0H@
M("`@("`@(")W(CH@,3(L#0H@("`@("`@(")X(CH@,"P-"B`@("`@("`@(GDB
M.B`X#0H@("`@("!]+`T*("`@("`@(FED(CH@,S`L#0H@("`@("`B:6YT97)V
M86PB.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@("`@(")L96=E
M;F0B.B![#0H@("`@("`@("`@(F-A;&-S(CH@6UTL#0H@("`@("`@("`@(F1I
M<W!L87E-;V1E(CH@(FQI<W0B+`T*("`@("`@("`@(")P;&%C96UE;G0B.B`B
M8F]T=&]M(BP-"B`@("`@("`@("`B<VAO=TQE9V5N9"(Z('1R=64-"B`@("`@
M("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@("`@(FUO9&4B
M.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R="(Z(")N;VYE(@T*("`@("`@
M("!]#0H@("`@("!]+`T*("`@("`@(G1A<F=E=',B.B!;#0H@("`@("`@('L-
M"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F1A=&%B
M87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*
M("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO
M<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S
M;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S
M<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@
M("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B
M.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B
M.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@
M("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@
M("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B
M.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@
M("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB
M.B`B-2XP+C4B+`T*("`@("`@("`@(")Q=65R>2(Z(")!<&ES97)V97)297%U
M97-T5&]T86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\
M('=H97)E($-L=7-T97(@/3T@7"(D0VQU<W1E<EPB7&Y\(&EN=F]K92!P<F]M
M7V1E;'1A*"E<;GP@97AT96YD($-O9&4]=&]S=')I;F<H3&%B96QS+F-O9&4I
M7&Y\('-U;6UA<FEZ92!686QU93US=6TH5F%L=64I(&)Y(&)I;BA4:6UE<W1A
M;7`L("1?7W1I;65);G1E<G9A;"DL($-O9&5<;GP@<')O:F5C="!4:6UE<W1A
M;7`L($-O9&4L(%9A;'5E7&Y\(&]R9&5R(&)Y(%1I;65S=&%M<"!A<V-<;B(L
M#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@
M(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U
M92P-"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@("`@("`@("`@(G)E<W5L
M=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@("`@?0T*("`@("`@72P-
M"B`@("`@(")T:71L92(Z(")!4$D@4V5R=F5R("T@4F5Q=65S=',@0GD@0V]D
M92(L#0H@("`@("`B='EP92(Z(")T:6UE<V5R:65S(@T*("`@('TL#0H@("`@
M>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R
M869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@
M("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("!]+`T*("`@
M("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@
M("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B<&%L
M971T92UC;&%S<VEC(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F-U<W1O
M;2(Z('L-"B`@("`@("`@("`@(")A>&ES0F]R9&5R4VAO=R(Z(&9A;'-E+`T*
M("`@("`@("`@("`@(F%X:7-#96YT97)E9%IE<F\B.B!F86QS92P-"B`@("`@
M("`@("`@(")A>&ES0V]L;W)-;V1E(CH@(G1E>'0B+`T*("`@("`@("`@("`@
M(F%X:7-,86)E;"(Z("(B+`T*("`@("`@("`@("`@(F%X:7-0;&%C96UE;G0B
M.B`B875T;R(L#0H@("`@("`@("`@("`B8F%R06QI9VYM96YT(CH@,"P-"B`@
M("`@("`@("`@(")D<F%W4W1Y;&4B.B`B;&EN92(L#0H@("`@("`@("`@("`B
M9FEL;$]P86-I='DB.B`P+`T*("`@("`@("`@("`@(F=R861I96YT36]D92(Z
M(")N;VYE(BP-"B`@("`@("`@("`@(")H:61E1G)O;2(Z('L-"B`@("`@("`@
M("`@("`@(FQE9V5N9"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=&]O;'1I
M<"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=FEZ(CH@9F%L<V4-"B`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`B:6YS97)T3G5L;',B.B!F86QS92P-
M"B`@("`@("`@("`@(")L:6YE26YT97)P;VQA=&EO;B(Z(")L:6YE87(B+`T*
M("`@("`@("`@("`@(FQI;F57:61T:"(Z(#$L#0H@("`@("`@("`@("`B<&]I
M;G13:7IE(CH@-2P-"B`@("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z
M('L-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@
M("`@?2P-"B`@("`@("`@("`@(")S:&]W4&]I;G1S(CH@(F%U=&\B+`T*("`@
M("`@("`@("`@(G-P86Y.=6QL<R(Z(&9A;'-E+`T*("`@("`@("`@("`@(G-T
M86-K:6YG(CH@>PT*("`@("`@("`@("`@("`B9W)O=7`B.B`B02(L#0H@("`@
M("`@("`@("`@(")M;V1E(CH@(FYO;F4B#0H@("`@("`@("`@("!]+`T*("`@
M("`@("`@("`@(G1H<F5S:&]L9'-3='EL92(Z('L-"B`@("`@("`@("`@("`@
M(FUO9&4B.B`B;V9F(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(FUA<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L
M9',B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@
M("`@("`@("`B<W1E<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@
M("`@("`@("`B8V]L;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V
M86QU92(Z(&YU;&P-"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@
M>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@
M("`@("`@(")V86QU92(Z(#@P#0H@("`@("`@("`@("`@('T-"B`@("`@("`@
M("`@(%T-"B`@("`@("`@("!]#0H@("`@("`@('TL#0H@("`@("`@(")O=F5R
M<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@>PT*("`@
M("`@("`B:"(Z(#@L#0H@("`@("`@(")W(CH@,3(L#0H@("`@("`@(")X(CH@
M,3(L#0H@("`@("`@(")Y(CH@.`T*("`@("`@?2P-"B`@("`@(")I9"(Z(#(L
M#0H@("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B.B![
M#0H@("`@("`@(")L96=E;F0B.B![#0H@("`@("`@("`@(F-A;&-S(CH@6UTL
M#0H@("`@("`@("`@(F1I<W!L87E-;V1E(CH@(FQI<W0B+`T*("`@("`@("`@
M(")P;&%C96UE;G0B.B`B8F]T=&]M(BP-"B`@("`@("`@("`B<VAO=TQE9V5N
M9"(Z('1R=64-"B`@("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@
M("`@("`@("`@(FUO9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R="(Z
M(")N;VYE(@T*("`@("`@("!]#0H@("`@("!]+`T*("`@("`@(G1A<F=E=',B
M.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@
M("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D
M871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA
M>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@
M(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@
M("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y
M(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@
M("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N
M<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@
M("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B
M86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@
M(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C4B+`T*("`@("`@("`@(")Q=65R>2(Z
M(")!<&ES97)V97)297%U97-T5&]T86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E
M<BA4:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T97(@/3T@7"(D0VQU<W1E<EPB
M7&Y\(&EN=F]K92!P<F]M7V1E;'1A*"E<;GP@97AT96YD(%9E<F(]=&]S=')I
M;F<H3&%B96QS+G9E<F(I7&Y\('-U;6UA<FEZ92!686QU93US=6TH5F%L=64I
M(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL(%9E<F)<;GP@
M<')O:F5C="!4:6UE<W1A;7`L(%9E<F(L(%9A;'5E7&Y\(&]R9&5R(&)Y(%1I
M;65S=&%M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A
M=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@
M(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B02(L#0H@
M("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@
M("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z(")!4$D@4V5R=F5R("T@
M4F5Q=65S=',@0GD@5F5R8B(L#0H@("`@("`B='EP92(Z(")T:6UE<V5R:65S
M(@T*("`@('TL#0H@("`@>PT*("`@("`@(F-O;&QA<'-E9"(Z(&9A;'-E+`T*
M("`@("`@(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@,2P-"B`@("`@("`@
M(G<B.B`R-"P-"B`@("`@("`@(G@B.B`P+`T*("`@("`@("`B>2(Z(#$V#0H@
M("`@("!]+`T*("`@("`@(FED(CH@."P-"B`@("`@(")P86YE;',B.B!;72P-
M"B`@("`@(")R97!E870B.B`B5F5R8B(L#0H@("`@("`B<F5P96%T1&ER96-T
M:6]N(CH@(F@B+`T*("`@("`@(G1I=&QE(CH@(B1697)B(BP-"B`@("`@(")T
M>7!E(CH@(G)O=R(-"B`@("!]+`T*("`@('L-"B`@("`@(")D871A<V]U<F-E
M(CH@>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP
M;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U
M<F-E.G1E>'1](@T*("`@("`@?2P-"B`@("`@(")F:65L9$-O;F9I9R(Z('L-
M"B`@("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@(")C;VQO<B(Z('L-
M"B`@("`@("`@("`@(")M;V1E(CH@(G!A;&5T=&4M8VQA<W-I8R(-"B`@("`@
M("`@("!]+`T*("`@("`@("`@(")C=7-T;VTB.B![#0H@("`@("`@("`@("`B
M87AI<T)O<F1E<E-H;W<B.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES0V5N
M=&5R961:97)O(CH@9F%L<V4L#0H@("`@("`@("`@("`B87AI<T-O;&]R36]D
M92(Z(")T97AT(BP-"B`@("`@("`@("`@(")A>&ES3&%B96PB.B`B(BP-"B`@
M("`@("`@("`@(")A>&ES4&QA8V5M96YT(CH@(F%U=&\B+`T*("`@("`@("`@
M("`@(F)A<D%L:6=N;65N="(Z(#`L#0H@("`@("`@("`@("`B9')A=U-T>6QE
M(CH@(FQI;F4B+`T*("`@("`@("`@("`@(F9I;&Q/<&%C:71Y(CH@,"P-"B`@
M("`@("`@("`@(")G<F%D:65N=$UO9&4B.B`B;F]N92(L#0H@("`@("`@("`@
M("`B:&ED949R;VTB.B![#0H@("`@("`@("`@("`@(")L96=E;F0B.B!F86QS
M92P-"B`@("`@("`@("`@("`@(G1O;VQT:7`B.B!F86QS92P-"B`@("`@("`@
M("`@("`@(G9I>B(Z(&9A;'-E#0H@("`@("`@("`@("!]+`T*("`@("`@("`@
M("`@(FEN<V5R=$YU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B;&EN94EN
M=&5R<&]L871I;VXB.B`B;&EN96%R(BP-"B`@("`@("`@("`@(")L:6YE5VED
M=&@B.B`Q+`T*("`@("`@("`@("`@(G!O:6YT4VEZ92(Z(#4L#0H@("`@("`@
M("`@("`B<V-A;&5$:7-T<FEB=71I;VXB.B![#0H@("`@("`@("`@("`@(")T
M>7!E(CH@(FQI;F5A<B(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M<VAO=U!O:6YT<R(Z(")A=71O(BP-"B`@("`@("`@("`@(")S<&%N3G5L;',B
M.B!F86QS92P-"B`@("`@("`@("`@(")S=&%C:VEN9R(Z('L-"B`@("`@("`@
M("`@("`@(F=R;W5P(CH@(D$B+`T*("`@("`@("`@("`@("`B;6]D92(Z(")N
M;VYE(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")T:')E<VAO;&1S
M4W1Y;&4B.B![#0H@("`@("`@("`@("`@(")M;V1E(CH@(F]F9B(-"B`@("`@
M("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")M87!P:6YG<R(Z
M(%M=+`T*("`@("`@("`@(")T:')E<VAO;&1S(CH@>PT*("`@("`@("`@("`@
M(FUO9&4B.B`B86)S;VQU=&4B+`T*("`@("`@("`@("`@(G-T97!S(CH@6PT*
M("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(F=R
M965N(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B
M8V]L;W(B.B`B<F5D(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*
M("`@("`@("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?0T*
M("`@("`@("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6UT-"B`@("`@('TL
M#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`X+`T*("`@("`@
M("`B=R(Z(#@L#0H@("`@("`@(")X(CH@,"P-"B`@("`@("`@(GDB.B`Q-PT*
M("`@("`@?2P-"B`@("`@(")I9"(Z(#$L#0H@("`@("`B:6YT97)V86PB.B`B
M,6TB+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@("`@(")L96=E;F0B.B![
M#0H@("`@("`@("`@(F-A;&-S(CH@6UTL#0H@("`@("`@("`@(F1I<W!L87E-
M;V1E(CH@(FQI<W0B+`T*("`@("`@("`@(")P;&%C96UE;G0B.B`B8F]T=&]M
M(BP-"B`@("`@("`@("`B<VAO=TQE9V5N9"(Z('1R=64-"B`@("`@("`@?2P-
M"B`@("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@("`@(FUO9&4B.B`B<VEN
M9VQE(BP-"B`@("`@("`@("`B<V]R="(Z(")N;VYE(@T*("`@("`@("!]#0H@
M("`@("!]+`T*("`@("`@(G)E<&5A="(Z(")297-O=7)C92(L#0H@("`@("`B
M<F5P96%T1&ER96-T:6]N(CH@(G8B+`T*("`@("`@(G1A<F=E=',B.B!;#0H@
M("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@
M("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D871A<V]U
M<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD
M871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@("`@("`@
M("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y(CH@>PT*
M("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@
M("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@
M(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=
M+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@("`B97AP
M<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*
M("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@(G!L=6=I
M;E9E<G-I;VXB.B`B-2XP+C4B+`T*("`@("`@("`@(")Q=65R>2(Z(")!<&ES
M97)V97)297%U97-T5&]T86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE
M<W1A;7`I7&Y\('=H97)E($-L=7-T97(@/3T@7"(D0VQU<W1E<EPB7&Y\('=H
M97)E($QA8F5L<RYR97-O=7)C92!I;B`H)%)E<V]U<F-E*5QN?"!W:&5R92!,
M86)E;',N=F5R8B!I;B`H)%9E<F(I7&Y\(&EN=F]K92!P<F]M7V1E;'1A*"E<
M;GP@97AT96YD($-O9&4]=&]S=')I;F<H3&%B96QS+F-O9&4I7&Y\('-U;6UA
M<FEZ92!686QU93US=6TH5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I
M;65);G1E<G9A;"DL($-O9&5<;GP@<')O:F5C="!4:6UE<W1A;7`L($-O9&4L
M(%9A;'5E7&Y\(&]R9&5R(&)Y(%1I;65S=&%M<"!A<V-<;B(L#0H@("`@("`@
M("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP
M92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@
M("`@("`B<F5F260B.B`B02(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z
M(")T:6UE7W-E<FEE<R(-"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T
M:71L92(Z("(D4F5S;W5R8V4@0GD@0V]D92(L#0H@("`@("`B='EP92(Z(")T
M:6UE<V5R:65S(@T*("`@('TL#0H@("`@>PT*("`@("`@(F1A=&%S;W5R8V4B
M.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L
M;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R
M8V4Z=&5X='TB#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*
M("`@("`@("`B9&5F875L=',B.B![#0H@("`@("`@("`@(F-U<W1O;2(Z('L-
M"B`@("`@("`@("`@(")H:61E1G)O;2(Z('L-"B`@("`@("`@("`@("`@(FQE
M9V5N9"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=&]O;'1I<"(Z(&9A;'-E
M+`T*("`@("`@("`@("`@("`B=FEZ(CH@9F%L<V4-"B`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`B<V-A;&5$:7-T<FEB=71I;VXB.B![#0H@("`@("`@
M("`@("`@(")T>7!E(CH@(FQI;F5A<B(-"B`@("`@("`@("`@('T-"B`@("`@
M("`@("!]+`T*("`@("`@("`@(")F:65L9$UI;DUA>"(Z(&9A;'-E#0H@("`@
M("`@('TL#0H@("`@("`@(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@
M("`@(")G<FED4&]S(CH@>PT*("`@("`@("`B:"(Z(#@L#0H@("`@("`@(")W
M(CH@."P-"B`@("`@("`@(G@B.B`X+`T*("`@("`@("`B>2(Z(#$W#0H@("`@
M("!]+`T*("`@("`@(FED(CH@-"P-"B`@("`@(")O<'1I;VYS(CH@>PT*("`@
M("`@("`B8V%L8W5L871E(CH@9F%L<V4L#0H@("`@("`@(")C96QL1V%P(CH@
M,"P-"B`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@(")E>'!O;F5N="(Z
M(#`N-2P-"B`@("`@("`@("`B9FEL;"(Z(")D87)K+6]R86YG92(L#0H@("`@
M("`@("`@(FUO9&4B.B`B<V-H96UE(BP-"B`@("`@("`@("`B<F5V97)S92(Z
M(&9A;'-E+`T*("`@("`@("`@(")S8V%L92(Z(")E>'!O;F5N=&EA;"(L#0H@
M("`@("`@("`@(G-C:&5M92(Z(")/<F%N9V5S(BP-"B`@("`@("`@("`B<W1E
M<',B.B`S,@T*("`@("`@("!]+`T*("`@("`@("`B97AE;7!L87)S(CH@>PT*
M("`@("`@("`@(")C;VQO<B(Z(")R9V)A*#(U-2PP+#(U-2PP+C<I(@T*("`@
M("`@("!]+`T*("`@("`@("`B9FEL=&5R5F%L=65S(CH@>PT*("`@("`@("`@
M(")L92(Z(#%E+3D-"B`@("`@("`@?2P-"B`@("`@("`@(FQE9V5N9"(Z('L-
M"B`@("`@("`@("`B<VAO=R(Z('1R=64-"B`@("`@("`@?2P-"B`@("`@("`@
M(G)O=W-&<F%M92(Z('L-"B`@("`@("`@("`B;&%Y;W5T(CH@(F%U=&\B#0H@
M("`@("`@('TL#0H@("`@("`@(")T;V]L=&EP(CH@>PT*("`@("`@("`@(")M
M;V1E(CH@(G-I;F=L92(L#0H@("`@("`@("`@(G-H;W=#;VQO<E-C86QE(CH@
M9F%L<V4L#0H@("`@("`@("`@(GE(:7-T;V=R86TB.B!F86QS90T*("`@("`@
M("!]+`T*("`@("`@("`B>4%X:7,B.B![#0H@("`@("`@("`@(F%X:7-0;&%C
M96UE;G0B.B`B;&5F="(L#0H@("`@("`@("`@(G)E=F5R<V4B.B!F86QS90T*
M("`@("`@("!]#0H@("`@("!]+`T*("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B
M,3`N-"XQ,2(L#0H@("`@("`B<F5P96%T(CH@(E)E<V]U<F-E(BP-"B`@("`@
M(")R97!E871$:7)E8W1I;VXB.B`B=B(L#0H@("`@("`B=&%R9V5T<R(Z(%L-
M"B`@("`@("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@
M("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S
M;W5R8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E
M+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED
M(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@
M("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![
M#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@
M("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@
M("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@
M6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@
M('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E
M>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B
M#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU
M9VEN5F5R<VEO;B(Z("(U+C`N-2(L#0H@("`@("`@("`@(G%U97)Y(CH@(D%P
M:7-E<G9E<E)E<75E<W1$=7)A=&EO;E-E8V]N9'-"=6-K971<;GP@=VAE<F4@
M)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\(&5X=&5N9"!397)I97,]=&]R
M96%L*$QA8F5L<RYL92E<;GP@=VAE<F4@3&%B96QS+G)E<V]U<F-E(&EN("@D
M4F5S;W5R8V4I7&Y\('=H97)E($QA8F5L<RYV97)B(&EN("@D5F5R8BE<;GP@
M:6YV;VME('!R;VU?9&5L=&$H*5QN?"!S=6UM87)I>F4@5F%L=64]<W5M*%9A
M;'5E*2!B>2!B:6XH5&EM97-T86UP+"`Q;2DL(%-E<FEE<UQN?"!P<F]J96-T
M(%1I;65S=&%M<"P@4V5R:65S+"!686QU95QN?"!O<F1E<B!B>2!4:6UE<W1A
M;7`@9&5S8RP@4V5R:65S(&%S8UQN?"!E>'1E;F0@5F%L=64]8V%S92AP<F5V
M*%-E<FEE<RD@/"!397)I97,L(&EF9BA686QU92UP<F5V*%9A;'5E*2`^(#`L
M(%9A;'5E+7!R978H5F%L=64I+"!T;W)E86PH,"DI+"!686QU92E<;GP@<')O
M:F5C="!4:6UE<W1A;7`L('1O<W1R:6YG*%-E<FEE<RDL(%9A;'5E7&Y\(&]R
M9&5R(&)Y(%1I;65S=&%M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U
M<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*
M("`@("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B
M.B`B02(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE
M<R(-"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z("),871E
M;F-Y(BP-"B`@("`@(")T>7!E(CH@(FAE871M87`B#0H@("`@?2P-"B`@("![
M#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A
M9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@
M("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@('TL#0H@("`@
M("`B9FEE;&1#;VYF:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z('L-"B`@
M("`@("`@("`B8W5S=&]M(CH@>PT*("`@("`@("`@("`@(FAI9&5&<F]M(CH@
M>PT*("`@("`@("`@("`@("`B;&5G96YD(CH@9F%L<V4L#0H@("`@("`@("`@
M("`@(")T;V]L=&EP(CH@9F%L<V4L#0H@("`@("`@("`@("`@(")V:7HB.B!F
M86QS90T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")S8V%L941I<W1R
M:6)U=&EO;B(Z('L-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B;&EN96%R(@T*
M("`@("`@("`@("`@?0T*("`@("`@("`@('T-"B`@("`@("`@?2P-"B`@("`@
M("`@(F]V97)R:61E<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B
M.B![#0H@("`@("`@(")H(CH@."P-"B`@("`@("`@(G<B.B`X+`T*("`@("`@
M("`B>"(Z(#$V+`T*("`@("`@("`B>2(Z(#$W#0H@("`@("!]+`T*("`@("`@
M(FED(CH@-RP-"B`@("`@(")I;G1E<G9A;"(Z("(Q;2(L#0H@("`@("`B;W!T
M:6]N<R(Z('L-"B`@("`@("`@(F-A;&-U;&%T92(Z(&9A;'-E+`T*("`@("`@
M("`B8V5L;$=A<"(Z(#$L#0H@("`@("`@(")C;VQO<B(Z('L-"B`@("`@("`@
M("`B97AP;VYE;G0B.B`P+C4L#0H@("`@("`@("`@(F9I;&PB.B`B9&%R:RUO
M<F%N9V4B+`T*("`@("`@("`@(")M;V1E(CH@(G-C:&5M92(L#0H@("`@("`@
M("`@(G)E=F5R<V4B.B!F86QS92P-"B`@("`@("`@("`B<V-A;&4B.B`B97AP
M;VYE;G1I86PB+`T*("`@("`@("`@(")S8VAE;64B.B`B3W)A;F=E<R(L#0H@
M("`@("`@("`@(G-T97!S(CH@-C0-"B`@("`@("`@?2P-"B`@("`@("`@(F5X
M96UP;&%R<R(Z('L-"B`@("`@("`@("`B8V]L;W(B.B`B<F=B82@R-34L,"PR
M-34L,"XW*2(-"B`@("`@("`@?2P-"B`@("`@("`@(F9I;'1E<E9A;'5E<R(Z
M('L-"B`@("`@("`@("`B;&4B.B`Q92TY#0H@("`@("`@('TL#0H@("`@("`@
M(")L96=E;F0B.B![#0H@("`@("`@("`@(G-H;W<B.B!T<G5E#0H@("`@("`@
M('TL#0H@("`@("`@(")R;W=S1G)A;64B.B![#0H@("`@("`@("`@(FQA>6]U
M="(Z(")A=71O(@T*("`@("`@("!]+`T*("`@("`@("`B=&]O;'1I<"(Z('L-
M"B`@("`@("`@("`B;6]D92(Z(")S:6YG;&4B+`T*("`@("`@("`@(")S:&]W
M0V]L;W)38V%L92(Z(&9A;'-E+`T*("`@("`@("`@(")Y2&ES=&]G<F%M(CH@
M9F%L<V4-"B`@("`@("`@?2P-"B`@("`@("`@(GE!>&ES(CH@>PT*("`@("`@
M("`@(")A>&ES4&QA8V5M96YT(CH@(FQE9G0B+`T*("`@("`@("`@(")R979E
M<G-E(CH@9F%L<V4L#0H@("`@("`@("`@(G5N:70B.B`B9&5C8GET97,B#0H@
M("`@("`@('T-"B`@("`@('TL#0H@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(Q
M,"XT+C$Q(BP-"B`@("`@(")R97!E870B.B`B4F5S;W5R8V4B+`T*("`@("`@
M(G)E<&5A=$1I<F5C=&EO;B(Z(")V(BP-"B`@("`@(")T87)G971S(CH@6PT*
M("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@
M("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O
M=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M
M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B
M.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@("`@('TL#0H@("`@("`@
M("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-
M"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@
M("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;
M72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@
M?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X
M<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-
M"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G
M:6Y697)S:6]N(CH@(C4N,"XU(BP-"B`@("`@("`@("`B<75E<GDB.B`B07!I
M<V5R=F5R4F5S<&]N<V53:7IE<T)U8VME=%QN?"!W:&5R92`D7U]T:6UE1FEL
M=&5R*%1I;65S=&%M<"E<;GP@97AT96YD(%-E<FEE<SUT;W)E86PH3&%B96QS
M+FQE*5QN?"!W:&5R92!,86)E;',N<F5S;W5R8V4@:6X@*"1297-O=7)C92E<
M;GP@=VAE<F4@3&%B96QS+G9E<F(@:6X@*"1697)B*5QN?"!I;G9O:V4@<')O
M;5]D96QT82@I7&Y\('-U;6UA<FEZ92!686QU93US=6TH5F%L=64I(&)Y(&)I
M;BA4:6UE<W1A;7`L(#%M*2P@4V5R:65S7&Y\('!R;VIE8W0@5&EM97-T86UP
M+"!397)I97,L(%9A;'5E7&Y\(&]R9&5R(&)Y(%1I;65S=&%M<"!D97-C+"!3
M97)I97,@87-C7&Y\(&5X=&5N9"!686QU93UC87-E*'!R978H4V5R:65S*2`\
M(%-E<FEE<RP@:69F*%9A;'5E+7!R978H5F%L=64I(#X@,"P@5F%L=64M<')E
M=BA686QU92DL('1O<F5A;"@P*2DL(%9A;'5E*5QN?"!P<F]J96-T(%1I;65S
M=&%M<"P@=&]S=')I;F<H4V5R:65S*2P@5F%L=65<;GP@;W)D97(@8GD@5&EM
M97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W
M(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@
M(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")!(BP-"B`@
M("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?<V5R:65S(@T*("`@("`@
M("!]#0H@("`@("!=+`T*("`@("`@(G1I=&QE(CH@(E)E<W!O;G-E(%-I>F4B
M+`T*("`@("`@(G1Y<&4B.B`B:&5A=&UA<"(-"B`@("!]#0H@(%TL#0H@(")R
M969R97-H(CH@(B(L#0H@(")S8VAE;6%697)S:6]N(CH@,SDL#0H@(")T86=S
M(CH@6UTL#0H@(")T96UP;&%T:6YG(CH@>PT*("`@(")L:7-T(CH@6PT*("`@
M("`@>PT*("`@("`@("`B:&ED92(Z(#`L#0H@("`@("`@(")I;F-L=61E06QL
M(CH@9F%L<V4L#0H@("`@("`@(")M=6QT:2(Z(&9A;'-E+`T*("`@("`@("`B
M;F%M92(Z(")$871A<V]U<F-E(BP-"B`@("`@("`@(F]P=&EO;G,B.B!;72P-
M"B`@("`@("`@(G%U97)Y(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E
M<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G%U97)Y5F%L=64B.B`B(BP-"B`@
M("`@("`@(G)E9G)E<V@B.B`Q+`T*("`@("`@("`B<F5G97@B.B`B(BP-"B`@
M("`@("`@(G-K:7!5<FQ3>6YC(CH@9F%L<V4L#0H@("`@("`@(")T>7!E(CH@
M(F1A=&%S;W5R8V4B#0H@("`@("!]+`T*("`@("`@>PT*("`@("`@("`B9&%T
M87-O=7)C92(Z('L-"B`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E
M+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@('TL#0H@("`@("`@(")D
M969I;FET:6]N(CH@(D-O;G1A:6YE<D-P=55S86=E4V5C;VYD<U1O=&%L('P@
M=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I('P@9&ES=&EN8W0@0VQU
M<W1E<B(L#0H@("`@("`@(")H:61E(CH@,"P-"B`@("`@("`@(FEN8VQU9&5!
M;&PB.B!F86QS92P-"B`@("`@("`@(FUU;'1I(CH@9F%L<V4L#0H@("`@("`@
M(")N86UE(CH@(D-L=7-T97(B+`T*("`@("`@("`B;W!T:6]N<R(Z(%M=+`T*
M("`@("`@("`B<75E<GDB.B![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E
M+`T*("`@("`@("`@(")C;'5S=&5R57)I(CH@(B(L#0H@("`@("`@("`@(F1A
M=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@
M>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E
M>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*
M("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@
M("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N
M-2(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<D-P=55S86=E4V5C
M;VYD<U1O=&%L('P@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I('P@
M9&ES=&EN8W0@0VQU<W1E<B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@
M(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@
M("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T
M(CH@(G1A8FQE(@T*("`@("`@("!]+`T*("`@("`@("`B<F5F<F5S:"(Z(#$L
M#0H@("`@("`@(")R96=E>"(Z("(B+`T*("`@("`@("`B<VMI<%5R;%-Y;F,B
M.B!F86QS92P-"B`@("`@("`@(G-O<G0B.B`P+`T*("`@("`@("`B='EP92(Z
M(")Q=65R>2(-"B`@("`@('TL#0H@("`@("![#0H@("`@("`@(")C=7)R96YT
M(CH@>PT*("`@("`@("`@(")S96QE8W1E9"(Z('1R=64L#0H@("`@("`@("`@
M(G1E>'0B.B!;#0H@("`@("`@("`@("`B87!I<V5R=FEC97,B+`T*("`@("`@
M("`@("`@(F%S<VEG;B(L#0H@("`@("`@("`@("`B87-S:6=N:6UA9V4B+`T*
M("`@("`@("`@("`@(F%S<VEG;FUE=&%D871A(BP-"B`@("`@("`@("`@(")C
M97)T:69I8V%T97-I9VYI;F=R97%U97-T<R(L#0H@("`@("`@("`@("`B8VQU
M<W1E<G)O;&5B:6YD:6YG<R(L#0H@("`@("`@("`@("`B8VQU<W1E<G)O;&5S
M(BP-"B`@("`@("`@("`@(")C;VYF:6=M87!S(BP-"B`@("`@("`@("`@(")C
M;VYF:6=S(BP-"B`@("`@("`@("`@(")C;VYS=')A:6YT<&]D<W1A='5S97,B
M+`T*("`@("`@("`@("`@(F-O;G-T<F%I;G1T96UP;&%T97!O9'-T871U<V5S
M(BP-"B`@("`@("`@("`@(")C;VYS=')A:6YT=&5M<&QA=&5S(BP-"B`@("`@
M("`@("`@(")C;VYT<F]L;&5R<F5V:7-I;VYS(BP-"B`@("`@("`@("`@(")C
M<F]N:F]B<R(L#0H@("`@("`@("`@("`B8W-I9')I=F5R<R(L#0H@("`@("`@
M("`@("`B8W-I;F]D97,B+`T*("`@("`@("`@("`@(F-S:7-T;W)A9V5C87!A
M8VET:65S(BP-"B`@("`@("`@("`@(")C=7-T;VUR97-O=7)C961E9FEN:71I
M;VYS(BP-"B`@("`@("`@("`@(")D865M;VYS971S(BP-"B`@("`@("`@("`@
M(")D97!L;WEM96YT<R(L#0H@("`@("`@("`@("`B96YD<&]I;G1S(BP-"B`@
M("`@("`@("`@(")E;F1P;VEN='-L:6-E<R(L#0H@("`@("`@("`@("`B979E
M;G1S(BP-"B`@("`@("`@("`@(")E>'!A;G-I;VYT96UP;&%T92(L#0H@("`@
M("`@("`@("`B97AP86YS:6]N=&5M<&QA=&5P;V1S=&%T=7-E<R(L#0H@("`@
M("`@("`@("`B9FQO=W-C:&5M87,B+`T*("`@("`@("`@("`@(F9U;F-T:6]N
M<R(L#0H@("`@("`@("`@("`B:&]R:7IO;G1A;'!O9&%U=&]S8V%L97)S(BP-
M"B`@("`@("`@("`@(")I;F=R97-S8VQA<W-E<R(L#0H@("`@("`@("`@("`B
M:6YG<F5S<V5S(BP-"B`@("`@("`@("`@(")J;V)S(BP-"B`@("`@("`@("`@
M(")K.'-A>G5R96-U<W1O;6-O;G1A:6YE<F%L;&]W961I;6%G97,B+`T*("`@
M("`@("`@("`@(FLX<V%Z=7)E=C)C=7-T;VUC;VYT86EN97)A;&QO=V5D:6UA
M9V5S(BP-"B`@("`@("`@("`@(")L96%S97,B+`T*("`@("`@("`@("`@(FQI
M;6ET<F%N9V5S(BP-"B`@("`@("`@("`@(")M;V1I9GES970B+`T*("`@("`@
M("`@("`@(FUU=&%T:6YG=V5B:&]O:V-O;F9I9W5R871I;VYS(BP-"B`@("`@
M("`@("`@(")M=71A=&]R<&]D<W1A='5S97,B+`T*("`@("`@("`@("`@(FYA
M;65S<&%C97,B+`T*("`@("`@("`@("`@(FYE='=O<FMP;VQI8VEE<R(L#0H@
M("`@("`@("`@("`B;F]D96YE='=O<FMC;VYF:6=S(BP-"B`@("`@("`@("`@
M(")N;V1E<R(L#0H@("`@("`@("`@("`B<&5R<VES=&5N='9O;'5M96-L86EM
M<R(L#0H@("`@("`@("`@("`B<&5R<VES=&5N='9O;'5M97,B+`T*("`@("`@
M("`@("`@(G!O9&1I<W)U<'1I;VYB=61G971S(BP-"B`@("`@("`@("`@(")P
M;V1S(BP-"B`@("`@("`@("`@(")P;V1T96UP;&%T97,B+`T*("`@("`@("`@
M("`@(G!R:6]R:71Y8VQA<W-E<R(L#0H@("`@("`@("`@("`B<')I;W)I='EL
M979E;&-O;F9I9W5R871I;VYS(BP-"B`@("`@("`@("`@(")P<F]V:61E<G,B
M+`T*("`@("`@("`@("`@(G)E<&QI8V%S971S(BP-"B`@("`@("`@("`@(")R
M97!L:6-A=&EO;F-O;G1R;VQL97)S(BP-"B`@("`@("`@("`@(")R97-O=7)C
M97%U;W1A<R(L#0H@("`@("`@("`@("`B<F]L96)I;F1I;F=S(BP-"B`@("`@
M("`@("`@(")R;VQE<R(L#0H@("`@("`@("`@("`B<G5N=&EM96-L87-S97,B
M+`T*("`@("`@("`@("`@(G-E8W)E=',B+`T*("`@("`@("`@("`@(G-E<G9I
M8V5A8V-O=6YT<R(L#0H@("`@("`@("`@("`B<V5R=FEC97,B+`T*("`@("`@
M("`@("`@(G-T871E9G5L<V5T<R(L#0H@("`@("`@("`@("`B<W1O<F%G96-L
M87-S97,B+`T*("`@("`@("`@("`@(G-U8FIE8W1A8V-E<W-R979I97=S(BP-
M"B`@("`@("`@("`@(")S>6YC<V5T<R(L#0H@("`@("`@("`@("`B=&]K96YR
M979I97=S(BP-"B`@("`@("`@("`@(")V86QI9&%T:6YG861M:7-S:6]N<&]L
M:6-I97,B+`T*("`@("`@("`@("`@(G9A;&ED871I;F=A9&UI<W-I;VYP;VQI
M8WEB:6YD:6YG<R(L#0H@("`@("`@("`@("`B=F%L:61A=&EN9W=E8FAO;VMC
M;VYF:6=U<F%T:6]N<R(L#0H@("`@("`@("`@("`B=F]L=6UE871T86-H;65N
M=',B+`T*("`@("`@("`@("`@(G9O;'5M97-N87!S:&]T8VQA<W-E<R(L#0H@
M("`@("`@("`@("`B=F]L=6UE<VYA<'-H;W1C;VYT96YT<R(L#0H@("`@("`@
M("`@("`B=F]L=6UE<VYA<'-H;W1S(@T*("`@("`@("`@(%TL#0H@("`@("`@
M("`@(G9A;'5E(CH@6PT*("`@("`@("`@("`@(F%P:7-E<G9I8V5S(BP-"B`@
M("`@("`@("`@(")A<W-I9VXB+`T*("`@("`@("`@("`@(F%S<VEG;FEM86=E
M(BP-"B`@("`@("`@("`@(")A<W-I9VYM971A9&%T82(L#0H@("`@("`@("`@
M("`B8V5R=&EF:6-A=&5S:6=N:6YG<F5Q=65S=',B+`T*("`@("`@("`@("`@
M(F-L=7-T97)R;VQE8FEN9&EN9W,B+`T*("`@("`@("`@("`@(F-L=7-T97)R
M;VQE<R(L#0H@("`@("`@("`@("`B8V]N9FEG;6%P<R(L#0H@("`@("`@("`@
M("`B8V]N9FEG<R(L#0H@("`@("`@("`@("`B8V]N<W1R86EN='!O9'-T871U
M<V5S(BP-"B`@("`@("`@("`@(")C;VYS=')A:6YT=&5M<&QA=&5P;V1S=&%T
M=7-E<R(L#0H@("`@("`@("`@("`B8V]N<W1R86EN='1E;7!L871E<R(L#0H@
M("`@("`@("`@("`B8V]N=')O;&QE<G)E=FES:6]N<R(L#0H@("`@("`@("`@
M("`B8W)O;FIO8G,B+`T*("`@("`@("`@("`@(F-S:61R:79E<G,B+`T*("`@
M("`@("`@("`@(F-S:6YO9&5S(BP-"B`@("`@("`@("`@(")C<VES=&]R86=E
M8V%P86-I=&EE<R(L#0H@("`@("`@("`@("`B8W5S=&]M<F5S;W5R8V5D969I
M;FET:6]N<R(L#0H@("`@("`@("`@("`B9&%E;6]N<V5T<R(L#0H@("`@("`@
M("`@("`B9&5P;&]Y;65N=',B+`T*("`@("`@("`@("`@(F5N9'!O:6YT<R(L
M#0H@("`@("`@("`@("`B96YD<&]I;G1S;&EC97,B+`T*("`@("`@("`@("`@
M(F5V96YT<R(L#0H@("`@("`@("`@("`B97AP86YS:6]N=&5M<&QA=&4B+`T*
M("`@("`@("`@("`@(F5X<&%N<VEO;G1E;7!L871E<&]D<W1A='5S97,B+`T*
M("`@("`@("`@("`@(F9L;W=S8VAE;6%S(BP-"B`@("`@("`@("`@(")F=6YC
M=&EO;G,B+`T*("`@("`@("`@("`@(FAO<FEZ;VYT86QP;V1A=71O<V-A;&5R
M<R(L#0H@("`@("`@("`@("`B:6YG<F5S<V-L87-S97,B+`T*("`@("`@("`@
M("`@(FEN9W)E<W-E<R(L#0H@("`@("`@("`@("`B:F]B<R(L#0H@("`@("`@
M("`@("`B:SAS87IU<F5C=7-T;VUC;VYT86EN97)A;&QO=V5D:6UA9V5S(BP-
M"B`@("`@("`@("`@(")K.'-A>G5R978R8W5S=&]M8V]N=&%I;F5R86QL;W=E
M9&EM86=E<R(L#0H@("`@("`@("`@("`B;&5A<V5S(BP-"B`@("`@("`@("`@
M(")L:6UI=')A;F=E<R(L#0H@("`@("`@("`@("`B;6]D:69Y<V5T(BP-"B`@
M("`@("`@("`@(")M=71A=&EN9W=E8FAO;VMC;VYF:6=U<F%T:6]N<R(L#0H@
M("`@("`@("`@("`B;75T871O<G!O9'-T871U<V5S(BP-"B`@("`@("`@("`@
M(")N86UE<W!A8V5S(BP-"B`@("`@("`@("`@(")N971W;W)K<&]L:6-I97,B
M+`T*("`@("`@("`@("`@(FYO9&5N971W;W)K8V]N9FEG<R(L#0H@("`@("`@
M("`@("`B;F]D97,B+`T*("`@("`@("`@("`@(G!E<G-I<W1E;G1V;VQU;65C
M;&%I;7,B+`T*("`@("`@("`@("`@(G!E<G-I<W1E;G1V;VQU;65S(BP-"B`@
M("`@("`@("`@(")P;V1D:7-R=7!T:6]N8G5D9V5T<R(L#0H@("`@("`@("`@
M("`B<&]D<R(L#0H@("`@("`@("`@("`B<&]D=&5M<&QA=&5S(BP-"B`@("`@
M("`@("`@(")P<FEO<FET>6-L87-S97,B+`T*("`@("`@("`@("`@(G!R:6]R
M:71Y;&5V96QC;VYF:6=U<F%T:6]N<R(L#0H@("`@("`@("`@("`B<')O=FED
M97)S(BP-"B`@("`@("`@("`@(")R97!L:6-A<V5T<R(L#0H@("`@("`@("`@
M("`B<F5P;&EC871I;VYC;VYT<F]L;&5R<R(L#0H@("`@("`@("`@("`B<F5S
M;W5R8V5Q=6]T87,B+`T*("`@("`@("`@("`@(G)O;&5B:6YD:6YG<R(L#0H@
M("`@("`@("`@("`B<F]L97,B+`T*("`@("`@("`@("`@(G)U;G1I;65C;&%S
M<V5S(BP-"B`@("`@("`@("`@(")S96-R971S(BP-"B`@("`@("`@("`@(")S
M97)V:6-E86-C;W5N=',B+`T*("`@("`@("`@("`@(G-E<G9I8V5S(BP-"B`@
M("`@("`@("`@(")S=&%T969U;'-E=',B+`T*("`@("`@("`@("`@(G-T;W)A
M9V5C;&%S<V5S(BP-"B`@("`@("`@("`@(")S=6)J96-T86-C97-S<F5V:65W
M<R(L#0H@("`@("`@("`@("`B<WEN8W-E=',B+`T*("`@("`@("`@("`@(G1O
M:V5N<F5V:65W<R(L#0H@("`@("`@("`@("`B=F%L:61A=&EN9V%D;6ES<VEO
M;G!O;&EC:65S(BP-"B`@("`@("`@("`@(")V86QI9&%T:6YG861M:7-S:6]N
M<&]L:6-Y8FEN9&EN9W,B+`T*("`@("`@("`@("`@(G9A;&ED871I;F=W96)H
M;V]K8V]N9FEG=7)A=&EO;G,B+`T*("`@("`@("`@("`@(G9O;'5M96%T=&%C
M:&UE;G1S(BP-"B`@("`@("`@("`@(")V;VQU;65S;F%P<VAO=&-L87-S97,B
M+`T*("`@("`@("`@("`@(G9O;'5M97-N87!S:&]T8V]N=&5N=',B+`T*("`@
M("`@("`@("`@(G9O;'5M97-N87!S:&]T<R(-"B`@("`@("`@("!=#0H@("`@
M("`@('TL#0H@("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@(")T
M>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E
M(BP-"B`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C97TB#0H@("`@("`@
M('TL#0H@("`@("`@(")D969I;FET:6]N(CH@(D%P:7-E<G9E<E)E<75E<W14
M;W1A;%QN?"!W:&5R92!4:6UE<W1A;7`@/B!A9V\H,S!M*5QN?"!W:&5R92!#
M;'5S=&5R(#T](%PB)$-L=7-T97)<(EQN?"!W:&5R92!,86)E;',N<F5S;W5R
M8V4@(3T@7")<(EQN?"!D:7-T:6YC="!T;W-T<FEN9RA,86)E;',N<F5S;W5R
M8V4I(BP-"B`@("`@("`@(FAI9&4B.B`P+`T*("`@("`@("`B:6YC;'5D94%L
M;"(Z('1R=64L#0H@("`@("`@(")L86)E;"(Z("(B+`T*("`@("`@("`B;75L
M=&DB.B!T<G5E+`T*("`@("`@("`B;F%M92(Z(")297-O=7)C92(L#0H@("`@
M("`@(")O<'1I;VYS(CH@6UTL#0H@("`@("`@(")Q=65R>2(Z('L-"B`@("`@
M("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F-L=7-T97)5<FDB
M.B`B(BP-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@
M("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z
M('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@
M("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@
M("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B
M.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@
M("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@
M(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N
M9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P
M;'5G:6Y697)S:6]N(CH@(C4N,"XU(BP-"B`@("`@("`@("`B<75E<GDB.B`B
M07!I<V5R=F5R4F5Q=65S=%1O=&%L7&Y\('=H97)E(%1I;65S=&%M<"`^(&%G
M;R@S,&TI7&Y\('=H97)E($-L=7-T97(@/3T@7"(D0VQU<W1E<EPB7&Y\('=H
M97)E($QA8F5L<RYR97-O=7)C92`A/2!<(EPB7&Y\(&1I<W1I;F-T('1O<W1R
M:6YG*$QA8F5L<RYR97-O=7)C92DB+`T*("`@("`@("`@(")Q=65R>5-O=7)C
M92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@
M("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@("`@("`@("`@(G)E<W5L=$9O
M<FUA="(Z(")T86)L92(-"B`@("`@("`@?2P-"B`@("`@("`@(G)E9G)E<V@B
M.B`Q+`T*("`@("`@("`B<F5G97@B.B`B(BP-"B`@("`@("`@(G-K:7!5<FQ3
M>6YC(CH@9F%L<V4L#0H@("`@("`@(")S;W)T(CH@,2P-"B`@("`@("`@(G1Y
M<&4B.B`B<75E<GDB#0H@("`@("!]+`T*("`@("`@>PT*("`@("`@("`B8W5R
M<F5N="(Z('L-"B`@("`@("`@("`B<V5L96-T960B.B!F86QS92P-"B`@("`@
M("`@("`B=&5X="(Z(")'150B+`T*("`@("`@("`@(")V86QU92(Z(")'150B
M#0H@("`@("`@('TL#0H@("`@("`@(")H:61E(CH@,"P-"B`@("`@("`@(FEN
M8VQU9&5!;&PB.B!T<G5E+`T*("`@("`@("`B;75L=&DB.B!T<G5E+`T*("`@
M("`@("`B;F%M92(Z(")697)B(BP-"B`@("`@("`@(F]P=&EO;G,B.B!;#0H@
M("`@("`@("`@>PT*("`@("`@("`@("`@(G-E;&5C=&5D(CH@9F%L<V4L#0H@
M("`@("`@("`@("`B=&5X="(Z(")!;&PB+`T*("`@("`@("`@("`@(G9A;'5E
M(CH@(B1?7V%L;"(-"B`@("`@("`@("!]+`T*("`@("`@("`@('L-"B`@("`@
M("`@("`@(")S96QE8W1E9"(Z(&9A;'-E+`T*("`@("`@("`@("`@(G1E>'0B
M.B`B3$E35"(L#0H@("`@("`@("`@("`B=F%L=64B.B`B3$E35"(-"B`@("`@
M("`@("!]+`T*("`@("`@("`@('L-"B`@("`@("`@("`@(")S96QE8W1E9"(Z
M(&9A;'-E+`T*("`@("`@("`@("`@(G1E>'0B.B`B4%54(BP-"B`@("`@("`@
M("`@(")V86QU92(Z(")0550B#0H@("`@("`@("`@?2P-"B`@("`@("`@("![
M#0H@("`@("`@("`@("`B<V5L96-T960B.B!F86QS92P-"B`@("`@("`@("`@
M(")T97AT(CH@(E!/4U0B+`T*("`@("`@("`@("`@(G9A;'5E(CH@(E!/4U0B
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("![#0H@("`@("`@("`@("`B<V5L
M96-T960B.B!T<G5E+`T*("`@("`@("`@("`@(G1E>'0B.B`B1T54(BP-"B`@
M("`@("`@("`@(")V86QU92(Z(")'150B#0H@("`@("`@("`@?2P-"B`@("`@
M("`@("![#0H@("`@("`@("`@("`B<V5L96-T960B.B!F86QS92P-"B`@("`@
M("`@("`@(")T97AT(CH@(E!!5$-((BP-"B`@("`@("`@("`@(")V86QU92(Z
M(")0051#2"(-"B`@("`@("`@("!]+`T*("`@("`@("`@('L-"B`@("`@("`@
M("`@(")S96QE8W1E9"(Z(&9A;'-E+`T*("`@("`@("`@("`@(G1E>'0B.B`B
M1$5,151%(BP-"B`@("`@("`@("`@(")V86QU92(Z(")$14Q%5$4B#0H@("`@
M("`@("`@?2P-"B`@("`@("`@("![#0H@("`@("`@("`@("`B<V5L96-T960B
M.B!F86QS92P-"B`@("`@("`@("`@(")T97AT(CH@(E=!5$-((BP-"B`@("`@
M("`@("`@(")V86QU92(Z(")7051#2"(-"B`@("`@("`@("!]+`T*("`@("`@
M("`@('L-"B`@("`@("`@("`@(")S96QE8W1E9"(Z(&9A;'-E+`T*("`@("`@
M("`@("`@(G1E>'0B.B`B0T].3D5#5"(L#0H@("`@("`@("`@("`B=F%L=64B
M.B`B0T].3D5#5"(-"B`@("`@("`@("!]#0H@("`@("`@(%TL#0H@("`@("`@
M(")Q=65R>2(Z("),25-4+%!55"Q03U-4+$=%5"Q0051#2"Q$14Q%5$4L5T%4
M0T@L0T].3D5#5"(L#0H@("`@("`@(")Q=65R>59A;'5E(CH@(B(L#0H@("`@
M("`@(")S:VEP57)L4WEN8R(Z(&9A;'-E+`T*("`@("`@("`B='EP92(Z(")C
M=7-T;VTB#0H@("`@("!]+`T*("`@("`@>PT*("`@("`@("`B9&%T87-O=7)C
M92(Z('L-"B`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M
M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@(G5I9"(Z("(D>T1A
M=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@('TL#0H@("`@("`@(")D969I;FET
M:6]N(CH@(G!R:6YT('-T<F-A="A<(FAT='!S.B\O7"(L(&-U<G)E;G1?8VQU
M<W1E<E]E;F1P;VEN="@I+"!<(B]<(BP@7"),;V=S7"(I(BP-"B`@("`@("`@
M(FAI9&4B.B`R+`T*("`@("`@("`B:6YC;'5D94%L;"(Z(&9A;'-E+`T*("`@
M("`@("`B;75L=&DB.B!F86QS92P-"B`@("`@("`@(FYA;64B.B`B3&]G<U52
M3"(L#0H@("`@("`@(")O<'1I;VYS(CH@6UTL#0H@("`@("`@(")Q=65R>2(Z
M('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F-L
M=7-T97)5<FDB.B`B(BP-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC
M<R(L#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B
M9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP
M<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*
M("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@
M("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T
M>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@
M("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N,"XU(BP-"B`@("`@("`@("`B
M<75E<GDB.B`B<')I;G0@<W1R8V%T*%PB:'1T<',Z+R]<(BP@8W5R<F5N=%]C
M;'5S=&5R7V5N9'!O:6YT*"DL(%PB+UPB+"!<(DQO9W-<(BDB+`T*("`@("`@
M("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R>51Y
M<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@("`@
M("`@("`@(G)E<W5L=$9O<FUA="(Z(")T86)L92(-"B`@("`@("`@?2P-"B`@
M("`@("`@(G)E9G)E<V@B.B`Q+`T*("`@("`@("`B<F5G97@B.B`B(BP-"B`@
M("`@("`@(G-K:7!5<FQ3>6YC(CH@9F%L<V4L#0H@("`@("`@(")S;W)T(CH@
M,"P-"B`@("`@("`@(G1Y<&4B.B`B<75E<GDB#0H@("`@("!]#0H@("`@70T*
M("!]+`T*("`B=&EM92(Z('L-"B`@("`B9G)O;2(Z(")N;W<M-6TB+`T*("`@
M(")T;R(Z(")N;W<B#0H@('TL#0H@(")T:6UE<&EC:V5R(CH@>WTL#0H@(")T
M:6UE>F]N92(Z(")B<F]W<V5R(BP-"B`@(G1I=&QE(CH@(D%022!397)V97(B
7+`T*("`B=V5E:U-T87)T(CH@(B(-"GT@
`
end
SHAR_EOF
  (set 20 24 12 06 20 59 58 'dashboards/api-server.json'
   eval "${shar_touch}") && \
  chmod 0644 'dashboards/api-server.json'
if test $? -ne 0
then ${echo} "restore of dashboards/api-server.json failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'dashboards/api-server.json': 'MD5 check failed'
       ) << \SHAR_EOF
06fae430cdd66df142f573c2513d00fd  dashboards/api-server.json
SHAR_EOF

else
test `LC_ALL=C wc -c < 'dashboards/api-server.json'` -ne 37778 && \
  ${echo} "restoration warning:  size of 'dashboards/api-server.json' is not 37778"
  fi
# ============= dashboards/namespaces.json ==============
  sed 's/^X//' << 'SHAR_EOF' | uudecode &&
begin 600 dashboards/namespaces.json
M>PT*("`B86YN;W1A=&EO;G,B.B![#0H@("`@(FQI<W0B.B!;#0H@("`@("![
M#0H@("`@("`@(")B=6EL=$EN(CH@,2P-"B`@("`@("`@(F1A=&%S;W5R8V4B
M.B![#0H@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X
M<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@(")U:60B.B`B)'M$871A
M<V]U<F-E.G1E>'1](@T*("`@("`@("!]+`T*("`@("`@("`B96YA8FQE(CH@
M=')U92P-"B`@("`@("`@(FAI9&4B.B!T<G5E+`T*("`@("`@("`B:6-O;D-O
M;&]R(CH@(G)G8F$H,"P@,C$Q+"`R-34L(#$I(BP-"B`@("`@("`@(FYA;64B
M.B`B06YN;W1A=&EO;G,@)B!!;&5R=',B+`T*("`@("`@("`B='EP92(Z(")D
M87-H8F]A<F0B#0H@("`@("!]#0H@("`@70T*("!]+`T*("`B961I=&%B;&4B
M.B!T<G5E+`T*("`B9FES8V%L665A<E-T87)T36]N=&@B.B`P+`T*("`B9W)A
M<&A4;V]L=&EP(CH@,"P-"B`@(FED(CH@-3`L#0H@(")L:6YK<R(Z(%L-"B`@
M("![#0H@("`@("`B87-$<F]P9&]W;B(Z(&9A;'-E+`T*("`@("`@(FEC;VXB
M.B`B97AT97)N86P@;&EN:R(L#0H@("`@("`B:6YC;'5D959A<G,B.B!T<G5E
M+`T*("`@("`@(FME97!4:6UE(CH@=')U92P-"B`@("`@(")T86=S(CH@6UTL
M#0H@("`@("`B=&%R9V5T0FQA;FLB.B!F86QS92P-"B`@("`@(")T:71L92(Z
M(")#;'5S=&5R(BP-"B`@("`@(")T;V]L=&EP(CH@(B(L#0H@("`@("`B='EP
M92(Z(")L:6YK(BP-"B`@("`@(")U<FPB.B`B:'1T<',Z+R]G<F%F86YA+6QA
M<F=E8VQU<W1E<BUD-&9F9W9D;F9T8G9C;69S+F-C82YG<F%F86YA+F%Z=7)E
M+F-O;2]D+V5D=C%L8FQE9W5U=W=B+V-L=7-T97(M:6YF;S]O<F=)9#TQ(@T*
M("`@('T-"B`@72P-"B`@(G!A;F5L<R(Z(%L-"B`@("![#0H@("`@("`B9&%T
M87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD
M871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[
M1&%T87-O=7)C93IT97AT?2(-"B`@("`@('TL#0H@("`@("`B9FEE;&1#;VYF
M:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z('L-"B`@("`@("`@("`B8V]L
M;W(B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")P86QE='1E+6-L87-S:6,B
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B8W5S=&]M(CH@>PT*("`@("`@
M("`@("`@(F%X:7-";W)D97)3:&]W(CH@9F%L<V4L#0H@("`@("`@("`@("`B
M87AI<T-E;G1E<F5D6F5R;R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X:7-#
M;VQO<DUO9&4B.B`B=&5X="(L#0H@("`@("`@("`@("`B87AI<TQA8F5L(CH@
M(B(L#0H@("`@("`@("`@("`B87AI<U!L86-E;65N="(Z(")A=71O(BP-"B`@
M("`@("`@("`@(")F:6QL3W!A8VET>2(Z(#<U+`T*("`@("`@("`@("`@(F=R
M861I96YT36]D92(Z(")N;VYE(BP-"B`@("`@("`@("`@(")H:61E1G)O;2(Z
M('L-"B`@("`@("`@("`@("`@(FQE9V5N9"(Z(&9A;'-E+`T*("`@("`@("`@
M("`@("`B=&]O;'1I<"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=FEZ(CH@
M9F%L<V4-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B;&EN95=I9'1H
M(CH@,"P-"B`@("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z('L-"B`@
M("`@("`@("`@("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")T:')E<VAO;&1S4W1Y;&4B.B![#0H@("`@("`@("`@
M("`@(")M;V1E(CH@(F]F9B(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]
M+`T*("`@("`@("`@(")L:6YK<R(Z(%L-"B`@("`@("`@("`@('L-"B`@("`@
M("`@("`@("`@(G1I=&QE(CH@(E9I97<@3F%M97-P86-E(BP-"B`@("`@("`@
M("`@("`@(G5R;"(Z(")H='1P<SHO+V=R869A;F$M;&%R9V5C;'5S=&5R+60T
M9F9G=F1N9G1B=F-M9G,N8V-A+F=R869A;F$N87IU<F4N8V]M+V0O9F1V9FIZ
M>#EL;V-U.&$O;F%M97-P86-E<S]O<F=)9#TQ)B1[7U]U<FQ?=&EM95]R86YG
M97TF)'M#;'5S=&5R.G%U97)Y<&%R86U])G9A<BU.86UE<W!A8V4])'M?7V9I
M96QD+FQA8F5L<RY.86UE<W!A8V5](@T*("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@('L-"B`@("`@("`@("`@("`@(G1I=&QE(CH@(E9I97<@4&]D<R(L
M#0H@("`@("`@("`@("`@(")U<FPB.B`B:'1T<',Z+R]G<F%F86YA+6QA<F=E
M8VQU<W1E<BUD-&9F9W9D;F9T8G9C;69S+F-C82YG<F%F86YA+F%Z=7)E+F-O
M;2]D+V-D=7AP.6YB;6HQ9FMB+W!O9',_;W)G260],29R969R97-H/3%M/TYA
M;65S<&%C93TD>U]?9FEE;&0N;&%B96QS+DYA;65S<&%C97TB#0H@("`@("`@
M("`@("!]#0H@("`@("`@("`@72P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;
M72P-"B`@("`@("`@("`B=&AR97-H;VQD<R(Z('L-"B`@("`@("`@("`@(")M
M;V1E(CH@(F%B<V]L=71E(BP-"B`@("`@("`@("`@(")S=&5P<R(Z(%L-"B`@
M("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")G<F5E
M;B(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@;G5L;`T*("`@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O
M;&]R(CH@(G)E9"(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@.#`-"B`@
M("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(G5N:70B.B`B0V]R97,B#0H@("`@("`@('TL#0H@("`@("`@
M(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@
M>PT*("`@("`@("`B:"(Z(#DL#0H@("`@("`@(")W(CH@,3(L#0H@("`@("`@
M(")X(CH@,"P-"B`@("`@("`@(GDB.B`P#0H@("`@("!]+`T*("`@("`@(FED
M(CH@,C$L#0H@("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO
M;G,B.B![#0H@("`@("`@(")B87)2861I=7,B.B`P+`T*("`@("`@("`B8F%R
M5VED=&@B.B`P+CDW+`T*("`@("`@("`B9G5L;$AI9VAL:6=H="(Z(&9A;'-E
M+`T*("`@("`@("`B9W)O=7!7:61T:"(Z(#`N-RP-"B`@("`@("`@(FQE9V5N
M9"(Z('L-"B`@("`@("`@("`B8V%L8W,B.B!;72P-"B`@("`@("`@("`B9&ES
M<&QA>4UO9&4B.B`B;&ES="(L#0H@("`@("`@("`@(G!L86-E;65N="(Z(")B
M;W1T;VTB+`T*("`@("`@("`@(")S:&]W3&5G96YD(CH@9F%L<V4-"B`@("`@
M("`@?2P-"B`@("`@("`@(F]R:65N=&%T:6]N(CH@(F%U=&\B+`T*("`@("`@
M("`B<VAO=U9A;'5E(CH@(F%L=V%Y<R(L#0H@("`@("`@(")S=&%C:VEN9R(Z
M(")N;W)M86PB+`T*("`@("`@("`B=&]O;'1I<"(Z('L-"B`@("`@("`@("`B
M;6]D92(Z(")S:6YG;&4B+`T*("`@("`@("`@(")S;W)T(CH@(FYO;F4B#0H@
M("`@("`@('TL#0H@("`@("`@(")X1FEE;&0B.B`B5&EM97-T86UP(BP-"B`@
M("`@("`@(GA4:6-K3&%B96Q2;W1A=&EO;B(Z(#`L#0H@("`@("`@(")X5&EC
M:TQA8F5L4W!A8VEN9R(Z(#$P,`T*("`@("`@?2P-"B`@("`@(")P;'5G:6Y6
M97)S:6]N(CH@(C$P+C0N-R(L#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@
M("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B
M9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B
M.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M
M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[
M1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E
M>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@
M("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B
M='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E
M9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@
M("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S
M:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@
M("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R
M<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE
M<D-P=55S86=E4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H
M5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@
M=VAE<F4@3&%B96QS+F-P=3T]7")T;W1A;%PB7&Y\('=H97)E($-O;G1A:6YE
M<B`]/2!<(F-A9'9I<V]R7")<;GP@97AT96YD($YA;65S<&%C93UT;W-T<FEN
M9RA,86)E;',N;F%M97-P86-E*5QN?"!W:&5R92!.86UE<W!A8V4@/3T@7"(D
M3F%M97-P86-E7")<;GP@97AT96YD(%!O9#UT;W-T<FEN9RA,86)E;',N<&]D
M*5QN?"!E>'1E;F0@0V]N=&%I;F5R/71O<W1R:6YG*$QA8F5L<RYC;VYT86EN
M97(I7&Y\('=H97)E($-O;G1A:6YE<B`]/2!<(EPB7&Y\(&5X=&5N9"!)9"`]
M('1O<W1R:6YG*$QA8F5L<RYI9"E<;GP@=VAE<F4@260@96YD<W=I=&@@7"(N
M<VQI8V5<(EQN?"!W:&5R92!.86UE<W!A8V4@(3T@7")<(EQN?"!I;G9O:V4@
M<')O;5]R871E*"E<;GP@<W5M;6%R:7IE(%9A;'5E/7)O=6YD*&%V9RA686QU
M92DK,"XP,#`U+#,I(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A
M;"DL($YA;65S<&%C92P@4&]D+"!)9%QN?"!E>'1E;F0@3F%M93UR97!L86-E
M7W)E9V5X*%!O9"P@7"(H+5MA+7HP+3E=>S5]*21<(BP@7")<(BE<;GP@<W5M
M;6%R:7IE(%9A;'5E/7-U;2A686QU92D@8GD@8FEN*%1I;65S=&%M<"P@)%]?
M=&EM94EN=&5R=F%L*2P@3F%M95QN?"!P<F]J96-T(%1I;65S=&%M<"P@3F%M
M92P@5F%L=65<;GP@;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@("`@
M("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4
M>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@
M("`@("`@(")R969)9"(Z(")5=&EL:7IA=&EO;B(L#0H@("`@("`@("`@(G)E
M<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@("`@?0T*("`@("`@
M72P-"B`@("`@(")T:71L92(Z(")#4%4@57-A9V4@0GD@5V]R:VQO860B+`T*
M("`@("`@(G1Y<&4B.B`B8F%R8VAA<G0B#0H@("`@?2P-"B`@("![#0H@("`@
M("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA
M>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED
M(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@('TL#0H@("`@("`B9FEE
M;&1#;VYF:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z('L-"B`@("`@("`@
M("`B8V]L;W(B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")P86QE='1E+6-L
M87-S:6,B#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B8W5S=&]M(CH@>PT*
M("`@("`@("`@("`@(F%X:7-";W)D97)3:&]W(CH@9F%L<V4L#0H@("`@("`@
M("`@("`B87AI<T-E;G1E<F5D6F5R;R(Z(&9A;'-E+`T*("`@("`@("`@("`@
M(F%X:7-#;VQO<DUO9&4B.B`B=&5X="(L#0H@("`@("`@("`@("`B87AI<TQA
M8F5L(CH@(B(L#0H@("`@("`@("`@("`B87AI<U!L86-E;65N="(Z(")A=71O
M(BP-"B`@("`@("`@("`@(")F:6QL3W!A8VET>2(Z(#<U+`T*("`@("`@("`@
M("`@(F=R861I96YT36]D92(Z(")N;VYE(BP-"B`@("`@("`@("`@(")H:61E
M1G)O;2(Z('L-"B`@("`@("`@("`@("`@(FQE9V5N9"(Z(&9A;'-E+`T*("`@
M("`@("`@("`@("`B=&]O;'1I<"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B
M=FEZ(CH@9F%L<V4-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B;&EN
M95=I9'1H(CH@,2P-"B`@("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z
M('L-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@
M("`@?2P-"B`@("`@("`@("`@(")T:')E<VAO;&1S4W1Y;&4B.B![#0H@("`@
M("`@("`@("`@(")M;V1E(CH@(F]F9B(-"B`@("`@("`@("`@('T-"B`@("`@
M("`@("!]+`T*("`@("`@("`@(")L:6YK<R(Z(%L-"B`@("`@("`@("`@('L-
M"B`@("`@("`@("`@("`@(G1I=&QE(CH@(E9I97<@4&]D<R(L#0H@("`@("`@
M("`@("`@(")U<FPB.B`B:'1T<',Z+R]G<F%F86YA+6QA<F=E8VQU<W1E<BUD
M-&9F9W9D;F9T8G9C;69S+F-C82YG<F%F86YA+F%Z=7)E+F-O;2]D+V-D=7AP
M.6YB;6HQ9FMB+W!O9',_;W)G260],29V87(M3F%M97-P86-E/21[7U]F:65L
M9"YL86)E;',N3F%M97-P86-E?2(-"B`@("`@("`@("`@('TL#0H@("`@("`@
M("`@("![#0H@("`@("`@("`@("`@(")T:71L92(Z(")6:65W($YA;65S<&%C
M92(L#0H@("`@("`@("`@("`@(")U<FPB.B`B:'1T<',Z+R]G<F%F86YA+6QA
M<F=E8VQU<W1E<BUD-&9F9W9D;F9T8G9C;69S+F-C82YG<F%F86YA+F%Z=7)E
M+F-O;2]D+V9D=F9J>G@Y;&]C=3AA+VYA;65S<&%C97,_;W)G260],28D>U]?
M=7)L7W1I;65?<F%N9V5])G9A<BU.86UE<W!A8V4])'M?7V9I96QD+FQA8F5L
M<RY.86UE<W!A8V5])B1[0VQU<W1E<CIQ=65R>7!A<F%M?2(-"B`@("`@("`@
M("`@('T-"B`@("`@("`@("!=+`T*("`@("`@("`@(")M87!P:6YG<R(Z(%M=
M+`T*("`@("`@("`@(")T:')E<VAO;&1S(CH@>PT*("`@("`@("`@("`@(FUO
M9&4B.B`B86)S;VQU=&4B+`T*("`@("`@("`@("`@(G-T97!S(CH@6PT*("`@
M("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(F=R965N
M(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@("`@
M("`@('TL#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L
M;W(B.B`B<F5D(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*("`@
M("`@("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@
M("`@("`@("`B=6YI="(Z(")B>71E<R(-"B`@("`@("`@?2P-"B`@("`@("`@
M(F]V97)R:61E<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![
M#0H@("`@("`@(")H(CH@.2P-"B`@("`@("`@(G<B.B`Q,BP-"B`@("`@("`@
M(G@B.B`Q,BP-"B`@("`@("`@(GDB.B`P#0H@("`@("!]+`T*("`@("`@(FED
M(CH@,C(L#0H@("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO
M;G,B.B![#0H@("`@("`@(")B87)2861I=7,B.B`P+`T*("`@("`@("`B8F%R
M5VED=&@B.B`P+CDW+`T*("`@("`@("`B9G5L;$AI9VAL:6=H="(Z(&9A;'-E
M+`T*("`@("`@("`B9W)O=7!7:61T:"(Z(#`N-RP-"B`@("`@("`@(FQE9V5N
M9"(Z('L-"B`@("`@("`@("`B8V%L8W,B.B!;72P-"B`@("`@("`@("`B9&ES
M<&QA>4UO9&4B.B`B;&ES="(L#0H@("`@("`@("`@(G!L86-E;65N="(Z(")B
M;W1T;VTB+`T*("`@("`@("`@(")S:&]W3&5G96YD(CH@9F%L<V4-"B`@("`@
M("`@?2P-"B`@("`@("`@(F]R:65N=&%T:6]N(CH@(F%U=&\B+`T*("`@("`@
M("`B<VAO=U9A;'5E(CH@(F%U=&\B+`T*("`@("`@("`B<W1A8VMI;F<B.B`B
M;F]R;6%L(BP-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@("`@("`@("`@(FUO
M9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R="(Z(")N;VYE(@T*("`@
M("`@("!]+`T*("`@("`@("`B>%1I8VM,86)E;%)O=&%T:6]N(CH@,"P-"B`@
M("`@("`@(GA4:6-K3&%B96Q3<&%C:6YG(CH@,3`P#0H@("`@("!]+`T*("`@
M("`@(G1A<F=E=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)
M(CH@9F%L<V4L#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*
M("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B
M.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*
M("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@
M("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@
M("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B
M.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@
M("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@
M(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A
M;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*
M("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@
M("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL
M#0H@("`@("`@("`@(FAI9&4B.B!T<G5E+`T*("`@("`@("`@(")P;'5G:6Y6
M97)S:6]N(CH@(C4N,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B0V]N=&%I
M;F5R365M;W)Y5V]R:VEN9U-E=$)Y=&5S7&Y\('=H97)E("1?7W1I;65&:6QT
M97(H5&EM97-T86UP*5QN?"!W:&5R92!,86)E;',@(6AA<R!<(FED7")<;GP@
M=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\(&5X=&5N9"!.86UE<W!A
M8V4]=&]S=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@=VAE<F4@3F%M97-P
M86-E(#T](%PB)$YA;65S<&%C95PB7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H
M3&%B96QS+G!O9"E<;GP@97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E
M;',N8V]N=&%I;F5R*5QN?"!W:&5R92!#;VYT86EN97(@(3T@7")<(EQN?"!E
M>'1E;F0@5F%L=64]5F%L=65<;GP@<W5M;6%R:7IE(%9A;'5E/6%V9RA686QU
M92D@8GD@8FEN*%1I;65S=&%M<"P@)%]?=&EM94EN=&5R=F%L*2P@3F%M97-P
M86-E+"!0;V0L($-O;G1A:6YE<EQN?"!S=6UM87)I>F4@5F%L=64]<W5M*%9A
M;'5E*2!B>2!B:6XH5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI+"!.86UE
M<W!A8V4L(%!O9%QN?"!S=6UM87)I>F4@5F%L=64]<W5M*%9A;'5E*2!B>2!B
M:6XH5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI+"!.86UE<W!A8V5<;GP@
M<')O:F5C="!4:6UE<W1A;7`L(%9A;'5E7&Y\(&]R9&5R(&)Y(%1I;65S=&%M
M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@
M("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-
M;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B571I;&EZ871I;VXB
M+`T*("`@("`@("`@(")R97-U;'1&;W)M870B.B`B=&EM95]S97)I97,B#0H@
M("`@("`@('TL#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L
M<V4L#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@
M("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A
M9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@
M("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@
M?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G
M<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-
M"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@
M("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y
M<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@
M("`@("`@(FAI9&4B.B!F86QS92P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO
M;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<DUE
M;6]R>5=O<FMI;F=3971">71E<UQN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I
M;65S=&%M<"E<;GP@=VAE<F4@3&%B96QS("%H87,@7")I9%PB7&Y\('=H97)E
M($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O
M<W1R:6YG*$QA8F5L<RYN86UE<W!A8V4I7&Y\('=H97)E($YA;65S<&%C92`]
M/2!<(B1.86UE<W!A8V5<(EQN?"!E>'1E;F0@4&]D/71O<W1R:6YG*$QA8F5L
M<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H3&%B96QS+F-O
M;G1A:6YE<BE<;GP@=VAE<F4@0V]N=&%I;F5R("$](%PB7")<;GP@97AT96YD
M(%9A;'5E/59A;'5E7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I(&)Y
M(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL($YA;65S<&%C92P@
M4&]D+"!#;VYT86EN97)<;GP@<W5M;6%R:7IE(%9A;'5E/7-U;2A686QU92D@
M8GD@8FEN*%1I;65S=&%M<"P@)%]?=&EM94EN=&5R=F%L*2P@3F%M97-P86-E
M+"!0;V1<;GP@97AT96YD($YA;64]<F5P;&%C95]R96=E>"A0;V0L(%PB*"U;
M82UZ,"TY77LU?2DD7"(L(%PB7"(I7&Y\('-U;6UA<FEZ92!686QU93US=6TH
M5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL($YA
M;65<;GP@<')O:F5C="!4:6UE<W1A;7`L($YA;64L(%9A;'5E7&Y\(&]R9&5R
M(&)Y(%1I;65S=&%M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E
M(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@
M("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B
M02(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-
M"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z(")-96UO<GD@
M57-A9V4@0GD@5V]R:VQO860B+`T*("`@("`@(G1Y<&4B.B`B8F%R8VAA<G0B
M#0H@("`@?2P-"B`@("![#0H@("`@("`B8V]L;&%P<V5D(CH@9F%L<V4L#0H@
M("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`Q+`T*("`@("`@("`B
M=R(Z(#(T+`T*("`@("`@("`B>"(Z(#`L#0H@("`@("`@(")Y(CH@.0T*("`@
M("`@?2P-"B`@("`@(")I9"(Z(#<L#0H@("`@("`B<&%N96QS(CH@6UTL#0H@
M("`@("`B=&ET;&4B.B`B0U!5(BP-"B`@("`@(")T>7!E(CH@(G)O=R(-"B`@
M("!]+`T*("`@('L-"B`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`B
M='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C
M92(L#0H@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@
M("`@?2P-"B`@("`@(")F:65L9$-O;F9I9R(Z('L-"B`@("`@("`@(F1E9F%U
M;'1S(CH@>PT*("`@("`@("`@(")C=7-T;VTB.B![#0H@("`@("`@("`@("`B
M:&ED949R;VTB.B![#0H@("`@("`@("`@("`@(")L96=E;F0B.B!F86QS92P-
M"B`@("`@("`@("`@("`@(G1O;VQT:7`B.B!F86QS92P-"B`@("`@("`@("`@
M("`@(G9I>B(Z(&9A;'-E#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(G-C86QE1&ES=')I8G5T:6]N(CH@>PT*("`@("`@("`@("`@("`B='EP92(Z
M(")L:6YE87(B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?0T*("`@("`@
M("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6UT-"B`@("`@('TL#0H@("`@
M("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`Q,"P-"B`@("`@("`@(G<B
M.B`Q,BP-"B`@("`@("`@(G@B.B`P+`T*("`@("`@("`B>2(Z(#$P#0H@("`@
M("!]+`T*("`@("`@(FED(CH@,34L#0H@("`@("`B:6YT97)V86PB.B`B,6TB
M+`T*("`@("`@(F]P=&EO;G,B.B![#0H@("`@("`@(")C86QC=6QA=&4B.B!F
M86QS92P-"B`@("`@("`@(F-A;&-U;&%T:6]N(CH@>PT*("`@("`@("`@(")X
M0G5C:V5T<R(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@(F-O=6YT(BP-"B`@
M("`@("`@("`@(")V86QU92(Z("(B#0H@("`@("`@("`@?0T*("`@("`@("!]
M+`T*("`@("`@("`B8V5L;$=A<"(Z(#$L#0H@("`@("`@(")C;VQO<B(Z('L-
M"B`@("`@("`@("`B97AP;VYE;G0B.B`P+C4L#0H@("`@("`@("`@(F9I;&PB
M.B`B9&%R:RUO<F%N9V4B+`T*("`@("`@("`@(")M;V1E(CH@(G-C:&5M92(L
M#0H@("`@("`@("`@(G)E=F5R<V4B.B!F86QS92P-"B`@("`@("`@("`B<V-A
M;&4B.B`B97AP;VYE;G1I86PB+`T*("`@("`@("`@(")S8VAE;64B.B`B3W)A
M;F=E<R(L#0H@("`@("`@("`@(G-T97!S(CH@-C0-"B`@("`@("`@?2P-"B`@
M("`@("`@(F5X96UP;&%R<R(Z('L-"B`@("`@("`@("`B8V]L;W(B.B`B<F=B
M82@R-34L,"PR-34L,"XW*2(-"B`@("`@("`@?2P-"B`@("`@("`@(F9I;'1E
M<E9A;'5E<R(Z('L-"B`@("`@("`@("`B;&4B.B`Q92TY#0H@("`@("`@('TL
M#0H@("`@("`@(")L96=E;F0B.B![#0H@("`@("`@("`@(G-H;W<B.B!T<G5E
M#0H@("`@("`@('TL#0H@("`@("`@(")R;W=S1G)A;64B.B![#0H@("`@("`@
M("`@(FQA>6]U="(Z(")A=71O(@T*("`@("`@("!]+`T*("`@("`@("`B=&]O
M;'1I<"(Z('L-"B`@("`@("`@("`B;6]D92(Z(")S:6YG;&4B+`T*("`@("`@
M("`@(")S:&]W0V]L;W)38V%L92(Z(&9A;'-E+`T*("`@("`@("`@(")Y2&ES
M=&]G<F%M(CH@9F%L<V4-"B`@("`@("`@?2P-"B`@("`@("`@(GE!>&ES(CH@
M>PT*("`@("`@("`@(")A>&ES4&QA8V5M96YT(CH@(FQE9G0B+`T*("`@("`@
M("`@(")R979E<G-E(CH@9F%L<V4-"B`@("`@("`@?0T*("`@("`@?2P-"B`@
M("`@(")P;'5G:6Y697)S:6]N(CH@(C$P+C0N,3$B+`T*("`@("`@(G1A<F=E
M=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L
M#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@
M(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N
M82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@
M("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-
M"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U
M<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@
M("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S
M:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@
M("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@
M("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B
M.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@
M("`@(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R
M>2(Z(")L970@8G5C:V5T<STU,#M<;FQE="!B:6Y3:7IE/71O<V-A;&%R*$-O
M;G1A:6YE<D-P=55S86=E4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?7W1I;65&
M:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R
M7")<;GP@=VAE<F4@3&%B96QS+F-P=3T]7")T;W1A;%PB7&Y\('=H97)E($QA
M8F5L<RYN86UE<W!A8V4@/3T@7"(D3F%M97-P86-E7")<;GP@97AT96YD($YA
M;65S<&%C93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@
M4&]D/71O<W1R:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]
M=&]S=')I;F<H3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@0V]N=&%I;F5R
M("$](%PB7")<;GP@:6YV;VME('!R;VU?9&5L=&$H*5QN?"!S=6UM87)I>F4@
M5F%L=64]*&UA>"A686QU92DO-C`I(&)Y(&)I;BA4:6UE<W1A;7`L(#8P,#`P
M;7,I+"!.86UE<W!A8V4L(%!O9%QN?"!S=6UM87)I>F4@;6EN*%9A;'5E*2P@
M;6%X*%9A;'5E*2P@879G*%9A;'5E*5QN?"!E>'1E;F0@1&EF9CTH;6%X7U9A
M;'5E("T@;6EN7U9A;'5E*2`O(&)U8VME='-<;GP@<')O:F5C="!$:69F*3M<
M;D-O;G1A:6YE<D-P=55S86=E4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?7W1I
M;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S
M=&5R7")<;GP@=VAE<F4@3&%B96QS+F-P=3T]7")T;W1A;%PB7&Y\('=H97)E
M($QA8F5L<RYN86UE<W!A8V4@/3T@7"(D3F%M97-P86-E7")<;GP@97AT96YD
M($YA;65S<&%C93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E
M;F0@4&]D/71O<W1R:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN
M97(]=&]S=')I;F<H3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@0V]N=&%I
M;F5R("$](%PB7")<;GP@:6YV;VME('!R;VU?9&5L=&$H*5QN?"!S=6UM87)I
M>F4@5F%L=64]*&UA>"A686QU92DO-C`I(&)Y(&)I;BA4:6UE<W1A;7`L(#8P
M,#`P;7,I+"!.86UE<W!A8V4L(%!O9%QN?"!S=6UM87)I>F4@5F%L=64]8V]U
M;G0H*2!B>2!4:6UE<W1A;7`L($)I;CUR;W5N9"AB:6XH5F%L=64L('1O<F5A
M;"AB:6Y3:7IE*2DL(#,I7&Y\(&]R9&5R(&)Y(%1I;65S=&%M<"!A<V-<;GP@
M<')O:F5C="!4:6UE<W1A;7`L('1O<W1R:6YG*$)I;BDL(%9A;'5E7&XB+`T*
M("`@("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@(")Q
M=65R>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R=64L
M#0H@("`@("`@("`@(G)E9DED(CH@(E5S86=E(BP-"B`@("`@("`@("`B<F5S
M=6QT1F]R;6%T(CH@(G1I;65?<V5R:65S(@T*("`@("`@("!]#0H@("`@("!=
M+`T*("`@("`@(G1I=&QE(CH@(D-052!5<V%G92`H0V]R97,I(BP-"B`@("`@
M(")T>7!E(CH@(FAE871M87`B#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T
M87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD
M871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[
M1&%T87-O=7)C93IT97AT?2(-"B`@("`@('TL#0H@("`@("`B9&5S8W)I<'1I
M;VXB.B`B(BP-"B`@("`@(")F:65L9$-O;F9I9R(Z('L-"B`@("`@("`@(F1E
M9F%U;'1S(CH@>PT*("`@("`@("`@(")C;VQO<B(Z('L-"B`@("`@("`@("`@
M(")M;V1E(CH@(G1H<F5S:&]L9',B#0H@("`@("`@("`@?2P-"B`@("`@("`@
M("`B8W5S=&]M(CH@>PT*("`@("`@("`@("`@(F%L:6=N(CH@(FQE9G0B+`T*
M("`@("`@("`@("`@(F-E;&Q/<'1I;VYS(CH@>PT*("`@("`@("`@("`@("`B
M='EP92(Z(")A=71O(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")F
M:6QT97)A8FQE(CH@9F%L<V4L#0H@("`@("`@("`@("`B:6YS<&5C="(Z('1R
M=64L#0H@("`@("`@("`@("`B;6EN5VED=&@B.B`Q-3`L#0H@("`@("`@("`@
M("`B=VED=&@B.B`Q,#`-"B`@("`@("`@("!]+`T*("`@("`@("`@(")F:65L
M9$UI;DUA>"(Z(&9A;'-E+`T*("`@("`@("`@(")M87!P:6YG<R(Z(%M=+`T*
M("`@("`@("`@(")T:')E<VAO;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B
M.B`B86)S;VQU=&4B+`T*("`@("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@
M("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-
M"B`@("`@("`@("`@("`@("`B=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@
M('TL#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B
M.B`B<F5D(BP-"B`@("`@("`@("`@("`@("`B=F%L=64B.B`X,`T*("`@("`@
M("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@
M("`@("`B=6YI="(Z(")N;VYE(@T*("`@("`@("!]+`T*("`@("`@("`B;W9E
M<G)I9&5S(CH@6PT*("`@("`@("`@('L-"B`@("`@("`@("`@(")M871C:&5R
M(CH@>PT*("`@("`@("`@("`@("`B:60B.B`B8GE.86UE(BP-"B`@("`@("`@
M("`@("`@(F]P=&EO;G,B.B`B4&]D<R(-"B`@("`@("`@("`@('TL#0H@("`@
M("`@("`@("`B<')O<&5R=&EE<R(Z(%L-"B`@("`@("`@("`@("`@>PT*("`@
M("`@("`@("`@("`@(")I9"(Z(")U;FET(BP-"B`@("`@("`@("`@("`@("`B
M=F%L=64B.B`B;F]N92(-"B`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@
M70T*("`@("`@("`@('TL#0H@("`@("`@("`@>PT*("`@("`@("`@("`@(FUA
M=&-H97(B.B![#0H@("`@("`@("`@("`@(")I9"(Z(")B>4YA;64B+`T*("`@
M("`@("`@("`@("`B;W!T:6]N<R(Z(").86UE(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")P<F]P97)T:65S(CH@6PT*("`@("`@("`@("`@("![
M#0H@("`@("`@("`@("`@("`@(FED(CH@(F-U<W1O;2YW:61T:"(-"B`@("`@
M("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@("`@
M("`@("`@>PT*("`@("`@("`@("`@(FUA=&-H97(B.B![#0H@("`@("`@("`@
M("`@(")I9"(Z(")B>4YA;64B+`T*("`@("`@("`@("`@("`B;W!T:6]N<R(Z
M(")0;V1S(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")P<F]P97)T
M:65S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(FED
M(CH@(F-U<W1O;2YC96QL3W!T:6]N<R(L#0H@("`@("`@("`@("`@("`@(G9A
M;'5E(CH@>PT*("`@("`@("`@("`@("`@("`@(FUO9&4B.B`B8F%S:6,B+`T*
M("`@("`@("`@("`@("`@("`@(G1Y<&4B.B`B9V%U9V4B+`T*("`@("`@("`@
M("`@("`@("`@(G9A;'5E1&ES<&QA>4UO9&4B.B`B=&5X="(-"B`@("`@("`@
M("`@("`@("!]#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@
M("`@("`@("!]+`T*("`@("`@("`@('L-"B`@("`@("`@("`@(")M871C:&5R
M(CH@>PT*("`@("`@("`@("`@("`B:60B.B`B8GE.86UE(BP-"B`@("`@("`@
M("`@("`@(F]P=&EO;G,B.B`B3F%M92(-"B`@("`@("`@("`@('TL#0H@("`@
M("`@("`@("`B<')O<&5R=&EE<R(Z(%L-"B`@("`@("`@("`@("`@>PT*("`@
M("`@("`@("`@("`@(")I9"(Z(")L:6YK<R(L#0H@("`@("`@("`@("`@("`@
M(G9A;'5E(CH@6PT*("`@("`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@
M("`@("`@("`B=&ET;&4B.B`B5FEE=R!P;V1S(BP-"B`@("`@("`@("`@("`@
M("`@("`@(G5R;"(Z(")H='1P<SHO+V=R869A;F$M;&%R9V5C;'5S=&5R+60T
M9F9G=F1N9G1B=F-M9G,N8V-A+F=R869A;F$N87IU<F4N8V]M+V0O8V1U>'`Y
M;F)M:C%F:V(O<&]D<S]O<F=)9#TQ)G)E9G)E<V@]-7,F4&]D/21[7U]V86QU
M92YT97AT?2(-"B`@("`@("`@("`@("`@("`@('T-"B`@("`@("`@("`@("`@
M("!=#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@("`@
M("!]#0H@("`@("`@(%T-"B`@("`@('TL#0H@("`@("`B9W)I9%!O<R(Z('L-
M"B`@("`@("`@(F@B.B`Q,"P-"B`@("`@("`@(G<B.B`Q,BP-"B`@("`@("`@
M(G@B.B`Q,BP-"B`@("`@("`@(GDB.B`Q,`T*("`@("`@?2P-"B`@("`@(")I
M9"(Z(#$W+`T*("`@("`@(FEN=&5R=F%L(CH@(C%M(BP-"B`@("`@(")O<'1I
M;VYS(CH@>PT*("`@("`@("`B8V5L;$AE:6=H="(Z(")S;2(L#0H@("`@("`@
M(")F;V]T97(B.B![#0H@("`@("`@("`@(F-O=6YT4F]W<R(Z(&9A;'-E+`T*
M("`@("`@("`@(")E;F%B;&5086=I;F%T:6]N(CH@9F%L<V4L#0H@("`@("`@
M("`@(F9I96QD<R(Z(%M=+`T*("`@("`@("`@(")R961U8V5R(CH@6PT*("`@
M("`@("`@("`@(G-U;2(-"B`@("`@("`@("!=+`T*("`@("`@("`@(")S:&]W
M(CH@=')U90T*("`@("`@("!]+`T*("`@("`@("`B<VAO=TAE861E<B(Z('1R
M=64L#0H@("`@("`@(")S;W)T0GDB.B!;#0H@("`@("`@("`@>PT*("`@("`@
M("`@("`@(F1E<V,B.B!T<G5E+`T*("`@("`@("`@("`@(F1I<W!L87E.86UE
M(CH@(DUA>"(-"B`@("`@("`@("!]#0H@("`@("`@(%T-"B`@("`@('TL#0H@
M("`@("`B<&QU9VEN5F5R<VEO;B(Z("(Q,"XT+C$Q(BP-"B`@("`@(")R97!E
M871$:7)E8W1I;VXB.B`B=B(L#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@
M("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B
M9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B
M.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M
M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[
M1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E
M>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@
M("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B
M='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E
M9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@
M("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S
M:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@
M("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R
M<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE
M<D-P=55S86=E4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H
M5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@
M=VAE<F4@3&%B96QS+F-P=3T]7")T;W1A;%PB7&Y\('=H97)E($QA8F5L<RYN
M86UE<W!A8V4@/3T@7"(D3F%M97-P86-E7")<;GP@97AT96YD($YA;65S<&%C
M93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O
M<W1R:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I
M;F<H3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@0V]N=&%I;F5R("$](%PB
M7")<;GP@:6YV;VME('!R;VU?9&5L=&$H*5QN?"!E>'1E;F0@3F%M93UR97!L
M86-E7W)E9V5X*%!O9"P@7"(H+5MA+7HP+3E=>S5]*21<(BP@7")<(BE<;GP@
M<W5M;6%R:7IE(%9A;'5E/6%V9RA686QU92\V,"DL($-O=6YT/61C;W5N="A0
M;V0I(&)Y(&)I;BA4:6UE<W1A;7`L(#8P,#`P;7,I+"!.86UE<W!A8V4L($YA
M;65<;GP@<W5M;6%R:7IE(%!O9',];6%X*$-O=6YT*2P@4#4P/7)O=6YD*'!E
M<F-E;G1I;&4H5F%L=64L(#`N-2DL,RDL(%`Y.3UR;W5N9"AP97)C96YT:6QE
M*%9A;'5E+"`P+CDY*2PS*2P@36EN/7)O=6YD*&UI;BA686QU92DL,RDL($UA
M>#UR;W5N9"AM87@H5F%L=64I+#,I(&)Y($YA;65S<&%C92P@3F%M95QN?"!P
M<F]J96-T+6%W87D@3F%M97-P86-E(BP-"B`@("`@("`@("`B<75E<GE3;W5R
M8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@
M("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z
M(")7;W)K:6YG4V5T(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1A
M8FQE(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1Y<&4B.B`B=&%B
M;&4B#0H@("`@?2P-"B`@("![#0H@("`@("`B8V]L;&%P<V5D(CH@9F%L<V4L
M#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`Q+`T*("`@("`@
M("`B=R(Z(#(T+`T*("`@("`@("`B>"(Z(#`L#0H@("`@("`@(")Y(CH@,C`-
M"B`@("`@('TL#0H@("`@("`B:60B.B`X+`T*("`@("`@(G!A;F5L<R(Z(%M=
M+`T*("`@("`@(G1I=&QE(CH@(DUE;6]R>2(L#0H@("`@("`B='EP92(Z(")R
M;W<B#0H@("`@?2P-"B`@("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@
M("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A
M=&%S;W5R8V4B+`T*("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT
M?2(-"B`@("`@('TL#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@("`@
M(")D969A=6QT<R(Z('L-"B`@("`@("`@("`B8W5S=&]M(CH@>PT*("`@("`@
M("`@("`@(FAI9&5&<F]M(CH@>PT*("`@("`@("`@("`@("`B;&5G96YD(CH@
M9F%L<V4L#0H@("`@("`@("`@("`@(")T;V]L=&EP(CH@9F%L<V4L#0H@("`@
M("`@("`@("`@(")V:7HB.B!F86QS90T*("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z('L-"B`@("`@("`@("`@("`@
M(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('T-
M"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R:61E<R(Z(%M=#0H@("`@("!]
M+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@,3`L#0H@("`@
M("`@(")W(CH@,3(L#0H@("`@("`@(")X(CH@,"P-"B`@("`@("`@(GDB.B`R
M,0T*("`@("`@?2P-"B`@("`@(")I9"(Z(#0L#0H@("`@("`B:6YT97)V86PB
M.B`B,6TB+`T*("`@("`@(FUA>%!E<E)O=R(Z(#(L#0H@("`@("`B;W!T:6]N
M<R(Z('L-"B`@("`@("`@(F-A;&-U;&%T92(Z(&9A;'-E+`T*("`@("`@("`B
M8V%L8W5L871I;VXB.B![#0H@("`@("`@("`@(GA"=6-K971S(CH@>PT*("`@
M("`@("`@("`@(FUO9&4B.B`B8V]U;G0B+`T*("`@("`@("`@("`@(G9A;'5E
M(CH@(C,R(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(GE"=6-K971S(CH@
M>PT*("`@("`@("`@("`@(FUO9&4B.B`B8V]U;G0B+`T*("`@("`@("`@("`@
M(G-C86QE(CH@>PT*("`@("`@("`@("`@("`B='EP92(Z(")L:6YE87(B#0H@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G9A;'5E(CH@(C8T(@T*("`@
M("`@("`@('T-"B`@("`@("`@?2P-"B`@("`@("`@(F-E;&Q'87`B.B`Q+`T*
M("`@("`@("`B8V]L;W(B.B![#0H@("`@("`@("`@(F5X<&]N96YT(CH@,"XU
M+`T*("`@("`@("`@(")F:6QL(CH@(F1A<FLM;W)A;F=E(BP-"B`@("`@("`@
M("`B;6]D92(Z(")S8VAE;64B+`T*("`@("`@("`@(")R979E<G-E(CH@9F%L
M<V4L#0H@("`@("`@("`@(G-C86QE(CH@(F5X<&]N96YT:6%L(BP-"B`@("`@
M("`@("`B<V-H96UE(CH@(D=R965N<R(L#0H@("`@("`@("`@(G-T97!S(CH@
M-C0-"B`@("`@("`@?2P-"B`@("`@("`@(F5X96UP;&%R<R(Z('L-"B`@("`@
M("`@("`B8V]L;W(B.B`B<F=B82@R-34L,"PR-34L,"XW*2(-"B`@("`@("`@
M?2P-"B`@("`@("`@(F9I;'1E<E9A;'5E<R(Z('L-"B`@("`@("`@("`B;&4B
M.B`Q92TY#0H@("`@("`@('TL#0H@("`@("`@(")L96=E;F0B.B![#0H@("`@
M("`@("`@(G-H;W<B.B!T<G5E#0H@("`@("`@('TL#0H@("`@("`@(")R;W=S
M1G)A;64B.B![#0H@("`@("`@("`@(FQA>6]U="(Z(")A=71O(@T*("`@("`@
M("!]+`T*("`@("`@("`B=&]O;'1I<"(Z('L-"B`@("`@("`@("`B;6]D92(Z
M(")S:6YG;&4B+`T*("`@("`@("`@(")S:&]W0V]L;W)38V%L92(Z(&9A;'-E
M+`T*("`@("`@("`@(")Y2&ES=&]G<F%M(CH@9F%L<V4-"B`@("`@("`@?2P-
M"B`@("`@("`@(GE!>&ES(CH@>PT*("`@("`@("`@(")A>&ES4&QA8V5M96YT
M(CH@(FQE9G0B+`T*("`@("`@("`@(")R979E<G-E(CH@9F%L<V4L#0H@("`@
M("`@("`@(G5N:70B.B`B9&5C8GET97,B#0H@("`@("`@('T-"B`@("`@('TL
M#0H@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(Q,"XT+C$Q(BP-"B`@("`@(")R
M97!E870B.B`B3F%M97-P86-E(BP-"B`@("`@(")R97!E871$:7)E8W1I;VXB
M.B`B=B(L#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@
M("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B.B`B
M365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@
M("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T
M87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT
M97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@
M>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E
M>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*
M("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@
M("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N
M,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<DUE;6]R>5=O<FMI
M;F=3971">71E<UQN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<
M;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\(&5X=&5N9"!.86UE
M<W!A8V4]=&]S=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@97AT96YD(%!O
M9#UT;W-T<FEN9RA,86)E;',N<&]D*5QN?"!E>'1E;F0@0V]N=&%I;F5R/71O
M<W1R:6YG*$QA8F5L<RYC;VYT86EN97(I7&Y\('=H97)E($YA;65S<&%C92`]
M/2!<(B1.86UE<W!A8V5<(EQN?"!E>'1E;F0@5F%L=64]5F%L=65<;GP@<W5M
M;6%R:7IE(%9A;'5E/6UA>"A686QU92D@8GD@8FEN*%1I;65S=&%M<"P@-C`P
M,#!M<RDL($YA;65S<&%C92P@4&]D7&Y\('-U;6UA<FEZ92!686QU93UC;W5N
M="@I(&)Y(%1I;65S=&%M<"P@0FEN/6)I;BA686QU92P@,C5E-BDL($YA;65S
M<&%C95QN?"!P<F]J96-T(%1I;65S=&%M<"P@=&]S=')I;F<H0FEN*2P@5F%L
M=65<;GP@;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B
M<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@
M(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@
M(")R969)9"(Z(")7;W)K:6YG4V5T(BP-"B`@("`@("`@("`B<F5S=6QT1F]R
M;6%T(CH@(G1I;65?<V5R:65S(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@
M("`@(G1Y<&4B.B`B:&5A=&UA<"(-"B`@("!]+`T*("`@('L-"B`@("`@(")D
M871A<V]U<F-E(CH@>PT*("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E
M+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@(")U:60B.B`B
M)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@?2P-"B`@("`@(")D97-C<FEP
M=&EO;B(Z("(B+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B
M9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@
M("`@(FUO9&4B.B`B=&AR97-H;VQD<R(-"B`@("`@("`@("!]+`T*("`@("`@
M("`@(")C=7-T;VTB.B![#0H@("`@("`@("`@("`B86QI9VXB.B`B;&5F="(L
M#0H@("`@("`@("`@("`B8V5L;$]P=&EO;G,B.B![#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%U=&\B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(FEN<W!E8W0B.B!F86QS92P-"B`@("`@("`@("`@(")M:6Y7:61T:"(Z(#$U
M,"P-"B`@("`@("`@("`@(")W:61T:"(Z(#$P,`T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(F9I96QD36EN36%X(CH@9F%L<V4L#0H@("`@("`@("`@(FUA
M<'!I;F=S(CH@6UTL#0H@("`@("`@("`@(G1H<F5S:&]L9',B.B![#0H@("`@
M("`@("`@("`B;6]D92(Z(")A8G-O;'5T92(L#0H@("`@("`@("`@("`B<W1E
M<',B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B8V]L
M;W(B.B`B9W)E96XB+`T*("`@("`@("`@("`@("`@(")V86QU92(Z(&YU;&P-
M"B`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@
M("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@("`@("`@("`@("`@(")V86QU
M92(Z(#@P#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@
M("`@("!]+`T*("`@("`@("`@(")U;FET(CH@(F1E8V)Y=&5S(@T*("`@("`@
M("!]+`T*("`@("`@("`B;W9E<G)I9&5S(CH@6PT*("`@("`@("`@('L-"B`@
M("`@("`@("`@(")M871C:&5R(CH@>PT*("`@("`@("`@("`@("`B:60B.B`B
M8GE.86UE(BP-"B`@("`@("`@("`@("`@(F]P=&EO;G,B.B`B4&]D<R(-"B`@
M("`@("`@("`@('TL#0H@("`@("`@("`@("`B<')O<&5R=&EE<R(Z(%L-"B`@
M("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")I9"(Z(")U;FET(BP-
M"B`@("`@("`@("`@("`@("`B=F%L=64B.B`B;F]N92(-"B`@("`@("`@("`@
M("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@("`@("`@("`@
M>PT*("`@("`@("`@("`@(FUA=&-H97(B.B![#0H@("`@("`@("`@("`@(")I
M9"(Z(")B>4YA;64B+`T*("`@("`@("`@("`@("`B;W!T:6]N<R(Z(").86UE
M(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")P<F]P97)T:65S(CH@
M6PT*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(FED(CH@(F-U
M<W1O;2YW:61T:"(-"B`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*
M("`@("`@("`@('TL#0H@("`@("`@("`@>PT*("`@("`@("`@("`@(FUA=&-H
M97(B.B![#0H@("`@("`@("`@("`@(")I9"(Z(")B>4YA;64B+`T*("`@("`@
M("`@("`@("`B;W!T:6]N<R(Z(")4;W1A;"(-"B`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`B<')O<&5R=&EE<R(Z(%L-"B`@("`@("`@("`@("`@>PT*
M("`@("`@("`@("`@("`@(")I9"(Z(")C=7-T;VTN8V5L;$]P=&EO;G,B+`T*
M("`@("`@("`@("`@("`@(")V86QU92(Z('L-"B`@("`@("`@("`@("`@("`@
M(")M;V1E(CH@(F=R861I96YT(BP-"B`@("`@("`@("`@("`@("`@(")T>7!E
M(CH@(F-O;&]R+6)A8VMG<F]U;F0B#0H@("`@("`@("`@("`@("`@?0T*("`@
M("`@("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@
M("`@("`@("![#0H@("`@("`@("`@("`B;6%T8VAE<B(Z('L-"B`@("`@("`@
M("`@("`@(FED(CH@(F)Y3F%M92(L#0H@("`@("`@("`@("`@(")O<'1I;VYS
M(CH@(E1O=&%L(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")P<F]P
M97)T:65S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@
M(FED(CH@(G1H<F5S:&]L9',B+`T*("`@("`@("`@("`@("`@(")V86QU92(Z
M('L-"B`@("`@("`@("`@("`@("`@(")M;V1E(CH@(G!E<F-E;G1A9V4B+`T*
M("`@("`@("`@("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@("`@("`@
M("`@("![#0H@("`@("`@("`@("`@("`@("`@("`@(F-O;&]R(CH@(G1R86YS
M<&%R96YT(BP-"B`@("`@("`@("`@("`@("`@("`@("`B=F%L=64B.B!N=6QL
M#0H@("`@("`@("`@("`@("`@("`@('TL#0H@("`@("`@("`@("`@("`@("`@
M('L-"B`@("`@("`@("`@("`@("`@("`@("`B8V]L;W(B.B`B<V5M:2UD87)K
M+6]R86YG92(L#0H@("`@("`@("`@("`@("`@("`@("`@(G9A;'5E(CH@-3`-
M"B`@("`@("`@("`@("`@("`@("`@?2P-"B`@("`@("`@("`@("`@("`@("`@
M>PT*("`@("`@("`@("`@("`@("`@("`@(")C;VQO<B(Z(")R960B+`T*("`@
M("`@("`@("`@("`@("`@("`@(")V86QU92(Z(#DP#0H@("`@("`@("`@("`@
M("`@("`@('T-"B`@("`@("`@("`@("`@("`@(%T-"B`@("`@("`@("`@("`@
M("!]#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@("`@
M("!]#0H@("`@("`@(%T-"B`@("`@('TL#0H@("`@("`B9W)I9%!O<R(Z('L-
M"B`@("`@("`@(F@B.B`Q,"P-"B`@("`@("`@(G<B.B`Q,BP-"B`@("`@("`@
M(G@B.B`Q,BP-"B`@("`@("`@(GDB.B`R,0T*("`@("`@?2P-"B`@("`@(")I
M9"(Z(#$V+`T*("`@("`@(FEN=&5R=F%L(CH@(C%M(BP-"B`@("`@(")O<'1I
M;VYS(CH@>PT*("`@("`@("`B8V5L;$AE:6=H="(Z(")S;2(L#0H@("`@("`@
M(")F;V]T97(B.B![#0H@("`@("`@("`@(F-O=6YT4F]W<R(Z(&9A;'-E+`T*
M("`@("`@("`@(")F:65L9',B.B!;#0H@("`@("`@("`@("`B4&]D<R(-"B`@
M("`@("`@("!=+`T*("`@("`@("`@(")R961U8V5R(CH@6PT*("`@("`@("`@
M("`@(G-U;2(-"B`@("`@("`@("!=+`T*("`@("`@("`@(")S:&]W(CH@=')U
M90T*("`@("`@("!]+`T*("`@("`@("`B<VAO=TAE861E<B(Z('1R=64L#0H@
M("`@("`@(")S;W)T0GDB.B!;#0H@("`@("`@("`@>PT*("`@("`@("`@("`@
M(F1E<V,B.B!T<G5E+`T*("`@("`@("`@("`@(F1I<W!L87E.86UE(CH@(DUA
M>"(-"B`@("`@("`@("!]#0H@("`@("`@(%T-"B`@("`@('TL#0H@("`@("`B
M<&QU9VEN5F5R<VEO;B(Z("(Q,"XT+C$Q(BP-"B`@("`@(")R97!E871$:7)E
M8W1I;VXB.B`B=B(L#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*
M("`@("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A
M<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@
M("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R
M97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O
M=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S
M:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@
M("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z
M(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z
M('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@
M("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@
M("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z
M(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@
M("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z
M("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<DUE;6]R
M>5=O<FMI;F=3971">71E<UQN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S
M=&%M<"E<;GP@=VAE<F4@3&%B96QS("%H87,@7")I9%PB7&Y\('=H97)E($-L
M=7-T97(]/5PB)$-L=7-T97)<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R
M:6YG*$QA8F5L<RYN86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H
M3&%B96QS+G!O9"E<;GP@97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E
M;',N8V]N=&%I;F5R*5QN?"!W:&5R92!.86UE<W!A8V4@/3T@7"(D3F%M97-P
M86-E7")<;GP@97AT96YD(%9A;'5E/59A;'5E7&Y\('-U;6UA<FEZ92!686QU
M93UM87@H5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A
M;"DL($YA;65S<&%C92P@4&]D7&Y\(&5X=&5N9"!.86UE/7)E<&QA8V5?<F5G
M97@H4&]D+"!<(B@M6V$M>C`M.5U[-7TI)%PB+"!<(EPB*5QN?"!S=6UM87)I
M>F4@4&]D<SUD8V]U;G0H4&]D*2P@4#4P/7!E<F-E;G1I;&4H5F%L=64L(#`N
M-2DL(%`Y.3UP97)C96YT:6QE*%9A;'5E+"`P+CDY*2P@36EN/6UI;BA686QU
M92DL($UA>#UM87@H5F%L=64I(&)Y($YA;65S<&%C92P@3F%M95QN?"!P<F]J
M96-T+6%W87D@3F%M97-P86-E(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B
M.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@
M("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")7
M;W)K:6YG4V5T(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1A8FQE
M(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1Y<&4B.B`B=&%B;&4B
M#0H@("`@?2P-"B`@("![#0H@("`@("`B8V]L;&%P<V5D(CH@9F%L<V4L#0H@
M("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`Q+`T*("`@("`@("`B
M=R(Z(#(T+`T*("`@("`@("`B>"(Z(#`L#0H@("`@("`@(")Y(CH@,S$-"B`@
M("`@('TL#0H@("`@("`B:60B.B`Y+`T*("`@("`@(G!A;F5L<R(Z(%M=+`T*
M("`@("`@(G1I=&QE(CH@(DYE='=O<FLB+`T*("`@("`@(G1Y<&4B.B`B<F]W
M(@T*("`@('TL#0H@("`@>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@
M("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A
M<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB
M#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B
M9&5F875L=',B.B![#0H@("`@("`@("`@(F-U<W1O;2(Z('L-"B`@("`@("`@
M("`@(")H:61E1G)O;2(Z('L-"B`@("`@("`@("`@("`@(FQE9V5N9"(Z(&9A
M;'-E+`T*("`@("`@("`@("`@("`B=&]O;'1I<"(Z(&9A;'-E+`T*("`@("`@
M("`@("`@("`B=FEZ(CH@9F%L<V4-"B`@("`@("`@("`@('TL#0H@("`@("`@
M("`@("`B<V-A;&5$:7-T<FEB=71I;VXB.B![#0H@("`@("`@("`@("`@(")T
M>7!E(CH@(FQI;F5A<B(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*
M("`@("`@("`@(")F:65L9$UI;DUA>"(Z(&9A;'-E#0H@("`@("`@('TL#0H@
M("`@("`@(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED
M4&]S(CH@>PT*("`@("`@("`B:"(Z(#$P+`T*("`@("`@("`B=R(Z(#$R+`T*
M("`@("`@("`B>"(Z(#`L#0H@("`@("`@(")Y(CH@,S(-"B`@("`@('TL#0H@
M("`@("`B:60B.B`U+`T*("`@("`@(FEN=&5R=F%L(CH@(C%M(BP-"B`@("`@
M(")O<'1I;VYS(CH@>PT*("`@("`@("`B8V%L8W5L871E(CH@9F%L<V4L#0H@
M("`@("`@(")C96QL1V%P(CH@,2P-"B`@("`@("`@(F-O;&]R(CH@>PT*("`@
M("`@("`@(")E>'!O;F5N="(Z(#`N-2P-"B`@("`@("`@("`B9FEL;"(Z(")D
M87)K+6]R86YG92(L#0H@("`@("`@("`@(FUO9&4B.B`B<V-H96UE(BP-"B`@
M("`@("`@("`B<F5V97)S92(Z(&9A;'-E+`T*("`@("`@("`@(")S8V%L92(Z
M(")E>'!O;F5N=&EA;"(L#0H@("`@("`@("`@(G-C:&5M92(Z(")29%EL0G4B
M+`T*("`@("`@("`@(")S=&5P<R(Z(#8T#0H@("`@("`@('TL#0H@("`@("`@
M(")E>&5M<&QA<G,B.B![#0H@("`@("`@("`@(F-O;&]R(CH@(G)G8F$H,C4U
M+#`L,C4U+#`N-RDB#0H@("`@("`@('TL#0H@("`@("`@(")F:6QT97)686QU
M97,B.B![#0H@("`@("`@("`@(FQE(CH@,64M.0T*("`@("`@("!]+`T*("`@
M("`@("`B;&5G96YD(CH@>PT*("`@("`@("`@(")S:&]W(CH@=')U90T*("`@
M("`@("!]+`T*("`@("`@("`B<F]W<T9R86UE(CH@>PT*("`@("`@("`@(")L
M87EO=70B.B`B875T;R(-"B`@("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B
M.B![#0H@("`@("`@("`@(FUO9&4B.B`B;75L=&DB+`T*("`@("`@("`@(")S
M:&]W0V]L;W)38V%L92(Z(&9A;'-E+`T*("`@("`@("`@(")Y2&ES=&]G<F%M
M(CH@9F%L<V4-"B`@("`@("`@?2P-"B`@("`@("`@(GE!>&ES(CH@>PT*("`@
M("`@("`@(")A>&ES4&QA8V5M96YT(CH@(FQE9G0B+`T*("`@("`@("`@(")R
M979E<G-E(CH@9F%L<V4L#0H@("`@("`@("`@(G5N:70B.B`B0G!S(@T*("`@
M("`@("!]#0H@("`@("!]+`T*("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B,3`N
M-"XQ,2(L#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@
M("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B.B`B
M365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@
M("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T
M87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT
M97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@
M>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E
M>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*
M("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@
M("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N
M,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<DYE='=O<FM296-E
M:79E0GET97-4;W1A;%QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M
M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\(&5X=&5N9"!.
M86UE<W!A8V4]=&]S=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@97AT96YD
M(%!O9#UT;W-T<FEN9RA,86)E;',N<&]D*5QN?"!E>'1E;F0@0V]N=&%I;F5R
M/71O<W1R:6YG*$QA8F5L<RYC;VYT86EN97(I7&Y\('=H97)E($YA;65S<&%C
M92`]/2!<(B1.86UE<W!A8V5<(EQN?"!I;G9O:V4@<')O;5]D96QT82@I7&Y\
M(&5X=&5N9"!686QU93U686QU92\V,%QN?"!S=6UM87)I>F4@5F%L=64]879G
M*%9A;'5E*2!B>2!B:6XH5&EM97-T86UP+"`V,#`P,&US*2P@3F%M97-P86-E
M+"!0;V1<;GP@<W5M;6%R:7IE(%9A;'5E/6-O=6YT*"D@8GD@5&EM97-T86UP
M+"!":6X]8FEN*%9A;'5E+"`Q,#(T*2P@3F%M97-P86-E7&Y\('!R;VIE8W0@
M5&EM97-T86UP+"!T;W-T<FEN9RA":6XI+"!686QU95QN?"!O<F1E<B!B>2!4
M:6UE<W1A;7`@87-C7&XO+R!\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I
M*BTQ(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL('1O<W1R
M:6YG*$QA8F5L<RYI9"DL(%!O9%QN+R\@?"!P<F]J96-T(%1I;65S=&%M<"P@
M4F5C:65V93U686QU95QN+R\@?"!O<F1E<B!B>2!4:6UE<W1A;7`@87-C7&XB
M+`T*("`@("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@
M(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R
M=64L#0H@("`@("`@("`@(G)E9DED(CH@(E)E8V5I=F4@0GET97,B+`T*("`@
M("`@("`@(")R97-U;'1&;W)M870B.B`B=&EM95]S97)I97,B#0H@("`@("`@
M('TL#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@
M("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D
M871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA
M>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@
M(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@
M("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y
M(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@
M("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N
M<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@
M("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B
M86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@
M(FAI9&4B.B!T<G5E+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N
M,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B0V]N=&%I;F5R3F5T=V]R:U1R
M86YS;6ET0GET97-4;W1A;%QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S
M=&%M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\(&5X=&5N
M9"!.86UE<W!A8V4]=&]S=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@97AT
M96YD(%!O9#UT;W-T<FEN9RA,86)E;',N<&]D*5QN?"!E>'1E;F0@0V]N=&%I
M;F5R/71O<W1R:6YG*$QA8F5L<RYC;VYT86EN97(I7&Y\('=H97)E($YA;65S
M<&%C92`]/2!<(B1.86UE<W!A8V5<(EQN?"!W:&5R92!0;V0@/3T@7"(D4&]D
M7")<;GP@:6YV;VME('!R;VU?9&5L=&$H*5QN?"!E>'1E;F0@5F%L=64]5F%L
M=64O-C!<;GP@<W5M;6%R:7IE(%9A;'5E/6%V9RA686QU92D@8GD@8FEN*%1I
M;65S=&%M<"P@)%]?=&EM94EN=&5R=F%L*2P@=&]S=')I;F<H3&%B96QS+FED
M*2P@4&]D7&Y\('!R;VIE8W0@5&EM97-T86UP+"!4<F%N<VUI=#U686QU95QN
M?"!O<F1E<B!B>2!4:6UE<W1A;7`@87-C7&XB+`T*("`@("`@("`@(")Q=65R
M>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R>51Y<&4B.B`B2U%,
M(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@("`@("`@("`@(G)E
M9DED(CH@(D$B+`T*("`@("`@("`@(")R97-U;'1&;W)M870B.B`B=&EM95]S
M97)I97,B#0H@("`@("`@('T-"B`@("`@(%TL#0H@("`@("`B=&ET;&4B.B`B
M3F5T=V]R:R!4:')O=6=H<'5T("A296-E:79E*2(L#0H@("`@("`B='EP92(Z
M(")H96%T;6%P(@T*("`@('TL#0H@("`@>PT*("`@("`@(F1A=&%S;W5R8V4B
M.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L
M;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R
M8V4Z=&5X='TB#0H@("`@("!]+`T*("`@("`@(F1E<V-R:7!T:6]N(CH@(B(L
M#0H@("`@("`B9FEE;&1#;VYF:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z
M('L-"B`@("`@("`@("`B8V]L;W(B.B![#0H@("`@("`@("`@("`B;6]D92(Z
M(")T:')E<VAO;&1S(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F-U<W1O
M;2(Z('L-"B`@("`@("`@("`@(")A;&EG;B(Z(")L969T(BP-"B`@("`@("`@
M("`@(")C96QL3W!T:6]N<R(Z('L-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B
M875T;R(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B:6YS<&5C="(Z
M(&9A;'-E+`T*("`@("`@("`@("`@(FUI;E=I9'1H(CH@,34P+`T*("`@("`@
M("`@("`@(G=I9'1H(CH@,3`P#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B
M9FEE;&1-:6Y-87@B.B!F86QS92P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;
M72P-"B`@("`@("`@("`B=&AR97-H;VQD<R(Z('L-"B`@("`@("`@("`@(")M
M;V1E(CH@(F%B<V]L=71E(BP-"B`@("`@("`@("`@(")S=&5P<R(Z(%L-"B`@
M("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")G<F5E
M;B(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@;G5L;`T*("`@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O
M;&]R(CH@(G)E9"(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@.#`-"B`@
M("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(G5N:70B.B`B9&5C8GET97,B#0H@("`@("`@('TL#0H@("`@
M("`@(")O=F5R<FED97,B.B!;#0H@("`@("`@("`@>PT*("`@("`@("`@("`@
M(FUA=&-H97(B.B![#0H@("`@("`@("`@("`@(")I9"(Z(")B>4YA;64B+`T*
M("`@("`@("`@("`@("`B;W!T:6]N<R(Z(")0;V1S(@T*("`@("`@("`@("`@
M?2P-"B`@("`@("`@("`@(")P<F]P97)T:65S(CH@6PT*("`@("`@("`@("`@
M("![#0H@("`@("`@("`@("`@("`@(FED(CH@(G5N:70B+`T*("`@("`@("`@
M("`@("`@(")V86QU92(Z(")N;VYE(@T*("`@("`@("`@("`@("!]#0H@("`@
M("`@("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@("`@("![#0H@("`@("`@
M("`@("`B;6%T8VAE<B(Z('L-"B`@("`@("`@("`@("`@(FED(CH@(F)Y3F%M
M92(L#0H@("`@("`@("`@("`@(")O<'1I;VYS(CH@(DYA;64B#0H@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@(G!R;W!E<G1I97,B.B!;#0H@("`@("`@
M("`@("`@('L-"B`@("`@("`@("`@("`@("`B:60B.B`B8W5S=&]M+G=I9'1H
M(@T*("`@("`@("`@("`@("!]#0H@("`@("`@("`@("!=#0H@("`@("`@("`@
M?0T*("`@("`@("!=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@
M("`@("`@(")H(CH@,3`L#0H@("`@("`@(")W(CH@,3(L#0H@("`@("`@(")X
M(CH@,3(L#0H@("`@("`@(")Y(CH@,S(-"B`@("`@('TL#0H@("`@("`B:60B
M.B`Q.2P-"B`@("`@(")I;G1E<G9A;"(Z("(Q;2(L#0H@("`@("`B;W!T:6]N
M<R(Z('L-"B`@("`@("`@(F-E;&Q(96EG:'0B.B`B<VTB+`T*("`@("`@("`B
M9F]O=&5R(CH@>PT*("`@("`@("`@(")C;W5N=%)O=W,B.B!F86QS92P-"B`@
M("`@("`@("`B9FEE;&1S(CH@(B(L#0H@("`@("`@("`@(G)E9'5C97(B.B!;
M#0H@("`@("`@("`@("`B<W5M(@T*("`@("`@("`@(%TL#0H@("`@("`@("`@
M(G-H;W<B.B!T<G5E#0H@("`@("`@('TL#0H@("`@("`@(")S:&]W2&5A9&5R
M(CH@=')U92P-"B`@("`@("`@(G-O<G1">2(Z(%L-"B`@("`@("`@("![#0H@
M("`@("`@("`@("`B9&5S8R(Z('1R=64L#0H@("`@("`@("`@("`B9&ES<&QA
M>4YA;64B.B`B36%X(@T*("`@("`@("`@('T-"B`@("`@("`@70T*("`@("`@
M?2P-"B`@("`@(")P;'5G:6Y697)S:6]N(CH@(C$P+C0N,3$B+`T*("`@("`@
M(G)E<&5A=$1I<F5C=&EO;B(Z(")V(BP-"B`@("`@(")T87)G971S(CH@6PT*
M("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@
M("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O
M=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M
M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B
M.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@("`@('TL#0H@("`@("`@
M("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-
M"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@
M("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;
M72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@
M?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X
M<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-
M"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G
M:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B0V]N
M=&%I;F5R3F5T=V]R:U)E8V5I=F5">71E<U1O=&%L7&Y\('=H97)E("1?7W1I
M;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S
M=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT;W-T<FEN9RA,86)E;',N;F%M
M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R:6YG*$QA8F5L<RYP;V0I7&Y\
M(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H3&%B96QS+F-O;G1A:6YE<BE<
M;GP@=VAE<F4@3F%M97-P86-E(#T](%PB)$YA;65S<&%C95PB7&Y\(&EN=F]K
M92!P<F]M7V1E;'1A*"E<;GP@97AT96YD(%9A;'5E/59A;'5E+S8P7&Y\('-U
M;6UA<FEZ92!686QU93UA=F<H5F%L=64I+"!#;W5N=#UD8V]U;G0H4&]D*2!B
M>2!B:6XH5&EM97-T86UP+"`V,#`P,&US*2P@3F%M97-P86-E+"!0;V1<;GP@
M97AT96YD($YA;64]<F5P;&%C95]R96=E>"A0;V0L(%PB*"U;82UZ,"TY77LU
M?2DD7"(L(%PB7"(I7&Y\('-U;6UA<FEZ92!0;V1S/6UA>"A#;W5N="DL(%`U
M,#UP97)C96YT:6QE*%9A;'5E+"`P+C4I+"!0.3D]<&5R8V5N=&EL92A686QU
M92P@,"XY.2DL($UI;CUM:6XH5F%L=64I+"!-87@];6%X*%9A;'5E*2!B>2!.
M86UE<W!A8V4L($YA;65<;GP@<')O:F5C="UA=V%Y($YA;65S<&%C92(L#0H@
M("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U
M97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-
M"B`@("`@("`@("`B<F5F260B.B`B5V]R:VEN9U-E="(L#0H@("`@("`@("`@
M(G)E<W5L=$9O<FUA="(Z(")T86)L92(-"B`@("`@("`@?0T*("`@("`@72P-
M"B`@("`@(")T>7!E(CH@(G1A8FQE(@T*("`@('TL#0H@("`@>PT*("`@("`@
M(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("!]+`T*("`@("`@(F9I96QD
M0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@("`@("`@("`@
M(F-U<W1O;2(Z('L-"B`@("`@("`@("`@(")H:61E1G)O;2(Z('L-"B`@("`@
M("`@("`@("`@(FQE9V5N9"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=&]O
M;'1I<"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=FEZ(CH@9F%L<V4-"B`@
M("`@("`@("`@('TL#0H@("`@("`@("`@("`B<V-A;&5$:7-T<FEB=71I;VXB
M.B![#0H@("`@("`@("`@("`@(")T>7!E(CH@(FQI;F5A<B(-"B`@("`@("`@
M("`@('T-"B`@("`@("`@("!]#0H@("`@("`@('TL#0H@("`@("`@(")O=F5R
M<FED97,B.B!;70T*("`@("`@?2P-"B`@("`@(")G<FED4&]S(CH@>PT*("`@
M("`@("`B:"(Z(#$P+`T*("`@("`@("`B=R(Z(#$R+`T*("`@("`@("`B>"(Z
M(#`L#0H@("`@("`@(")Y(CH@-#(-"B`@("`@('TL#0H@("`@("`B:60B.B`Q
M."P-"B`@("`@(")I;G1E<G9A;"(Z("(Q;2(L#0H@("`@("`B;W!T:6]N<R(Z
M('L-"B`@("`@("`@(F-A;&-U;&%T92(Z(&9A;'-E+`T*("`@("`@("`B8V5L
M;$=A<"(Z(#`L#0H@("`@("`@(")C;VQO<B(Z('L-"B`@("`@("`@("`B97AP
M;VYE;G0B.B`P+C4L#0H@("`@("`@("`@(F9I;&PB.B`B9&%R:RUO<F%N9V4B
M+`T*("`@("`@("`@(")M;V1E(CH@(G-C:&5M92(L#0H@("`@("`@("`@(G)E
M=F5R<V4B.B!F86QS92P-"B`@("`@("`@("`B<V-A;&4B.B`B97AP;VYE;G1I
M86PB+`T*("`@("`@("`@(")S8VAE;64B.B`B3W)A;F=E<R(L#0H@("`@("`@
M("`@(G-T97!S(CH@-C0-"B`@("`@("`@?2P-"B`@("`@("`@(F5X96UP;&%R
M<R(Z('L-"B`@("`@("`@("`B8V]L;W(B.B`B<F=B82@R-34L,"PR-34L,"XW
M*2(-"B`@("`@("`@?2P-"B`@("`@("`@(F9I;'1E<E9A;'5E<R(Z('L-"B`@
M("`@("`@("`B;&4B.B`Q92TY#0H@("`@("`@('TL#0H@("`@("`@(")L96=E
M;F0B.B![#0H@("`@("`@("`@(G-H;W<B.B!T<G5E#0H@("`@("`@('TL#0H@
M("`@("`@(")R;W=S1G)A;64B.B![#0H@("`@("`@("`@(FQA>6]U="(Z(")A
M=71O(@T*("`@("`@("!]+`T*("`@("`@("`B=&]O;'1I<"(Z('L-"B`@("`@
M("`@("`B;6]D92(Z(")S:6YG;&4B+`T*("`@("`@("`@(")S:&]W0V]L;W)3
M8V%L92(Z(&9A;'-E+`T*("`@("`@("`@(")Y2&ES=&]G<F%M(CH@9F%L<V4-
M"B`@("`@("`@?2P-"B`@("`@("`@(GE!>&ES(CH@>PT*("`@("`@("`@(")A
M>&ES4&QA8V5M96YT(CH@(FQE9G0B+`T*("`@("`@("`@(")R979E<G-E(CH@
M9F%L<V4L#0H@("`@("`@("`@(G5N:70B.B`B0G!S(@T*("`@("`@("!]#0H@
M("`@("!]+`T*("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B,3`N-"XQ,2(L#0H@
M("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@("`@(")/<&5N
M04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L
M#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@("`B='EP
M92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L
M#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@
M("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@
M("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N
M<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@
M("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@
M(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![
M#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@
M("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@
M?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@("`@
M("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<DYE='=O<FM4<F%N<VUI=$)Y=&5S
M5&]T86Q<;GP@=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H
M97)E($-L=7-T97(]/5PB)$-L=7-T97)<(EQN?"!E>'1E;F0@3F%M97-P86-E
M/71O<W1R:6YG*$QA8F5L<RYN86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S
M=')I;F<H3&%B96QS+G!O9"E<;GP@97AT96YD($-O;G1A:6YE<CUT;W-T<FEN
M9RA,86)E;',N8V]N=&%I;F5R*5QN?"!W:&5R92!.86UE<W!A8V4@/3T@7"(D
M3F%M97-P86-E7")<;GP@:6YV;VME('!R;VU?9&5L=&$H*5QN?"!E>'1E;F0@
M5F%L=64]5F%L=64O-C!<;GP@<W5M;6%R:7IE(%9A;'5E/6%V9RA686QU92D@
M8GD@8FEN*%1I;65S=&%M<"P@-C`P,#!M<RDL($YA;65S<&%C92P@4&]D7&Y\
M('-U;6UA<FEZ92!686QU93UC;W5N="@I(&)Y(%1I;65S=&%M<"P@0FEN/6)I
M;BA686QU92P@,3`R-"DL($YA;65S<&%C95QN?"!P<F]J96-T(%1I;65S=&%M
M<"P@=&]S=')I;F<H0FEN*2P@5F%L=65<;GP@;W)D97(@8GD@5&EM97-T86UP
M(&%S8UQN+R\@?"!S=6UM87)I>F4@5F%L=64]879G*%9A;'5E*2HM,2!B>2!B
M:6XH5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI+"!T;W-T<FEN9RA,86)E
M;',N:60I+"!0;V1<;B\O('P@<')O:F5C="!4:6UE<W1A;7`L(%)E8VEE=F4]
M5F%L=65<;B\O('P@;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@("`@
M("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4
M>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@
M("`@("`@(")R969)9"(Z(")296-E:79E($)Y=&5S(BP-"B`@("`@("`@("`B
M<F5S=6QT1F]R;6%T(CH@(G1I;65?<V5R:65S(@T*("`@("`@("!]#0H@("`@
M("!=+`T*("`@("`@(G1I=&QE(CH@(DYE='=O<FL@5&AR;W5G:'!U="`H5')A
M;G-M:70I(BP-"B`@("`@(")T>7!E(CH@(FAE871M87`B#0H@("`@?2P-"B`@
M("![#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B
M9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@
M("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@('TL#0H@
M("`@("`B9&5S8W)I<'1I;VXB.B`B(BP-"B`@("`@(")F:65L9$-O;F9I9R(Z
M('L-"B`@("`@("`@(F1E9F%U;'1S(CH@>PT*("`@("`@("`@(")C;VQO<B(Z
M('L-"B`@("`@("`@("`@(")M;V1E(CH@(G1H<F5S:&]L9',B#0H@("`@("`@
M("`@?2P-"B`@("`@("`@("`B8W5S=&]M(CH@>PT*("`@("`@("`@("`@(F%L
M:6=N(CH@(FQE9G0B+`T*("`@("`@("`@("`@(F-E;&Q/<'1I;VYS(CH@>PT*
M("`@("`@("`@("`@("`B='EP92(Z(")A=71O(@T*("`@("`@("`@("`@?2P-
M"B`@("`@("`@("`@(")I;G-P96-T(CH@9F%L<V4L#0H@("`@("`@("`@("`B
M;6EN5VED=&@B.B`Q-3`L#0H@("`@("`@("`@("`B=VED=&@B.B`Q,#`-"B`@
M("`@("`@("!]+`T*("`@("`@("`@(")F:65L9$UI;DUA>"(Z(&9A;'-E+`T*
M("`@("`@("`@(")M87!P:6YG<R(Z(%M=+`T*("`@("`@("`@(")T:')E<VAO
M;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B86)S;VQU=&4B+`T*("`@
M("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@("`@
M("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@("`@("`@("`B
M=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@('TL#0H@("`@("`@("`@("`@
M('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B<F5D(BP-"B`@("`@("`@
M("`@("`@("`B=F%L=64B.B`X,`T*("`@("`@("`@("`@("!]#0H@("`@("`@
M("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B=6YI="(Z(")D96-B
M>71E<R(-"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R:61E<R(Z(%L-"B`@
M("`@("`@("![#0H@("`@("`@("`@("`B;6%T8VAE<B(Z('L-"B`@("`@("`@
M("`@("`@(FED(CH@(F)Y3F%M92(L#0H@("`@("`@("`@("`@(")O<'1I;VYS
M(CH@(E!O9',B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G!R;W!E
M<G1I97,B.B!;#0H@("`@("`@("`@("`@('L-"B`@("`@("`@("`@("`@("`B
M:60B.B`B=6YI="(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@(FYO;F4B
M#0H@("`@("`@("`@("`@('T-"B`@("`@("`@("`@(%T-"B`@("`@("`@("!]
M+`T*("`@("`@("`@('L-"B`@("`@("`@("`@(")M871C:&5R(CH@>PT*("`@
M("`@("`@("`@("`B:60B.B`B8GE.86UE(BP-"B`@("`@("`@("`@("`@(F]P
M=&EO;G,B.B`B3F%M92(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M<')O<&5R=&EE<R(Z(%L-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@
M("`@(")I9"(Z(")C=7-T;VTN=VED=&@B#0H@("`@("`@("`@("`@('T-"B`@
M("`@("`@("`@(%T-"B`@("`@("`@("!]#0H@("`@("`@(%T-"B`@("`@('TL
M#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@("`@("`@(F@B.B`Q,"P-"B`@("`@
M("`@(G<B.B`Q,BP-"B`@("`@("`@(G@B.B`Q,BP-"B`@("`@("`@(GDB.B`T
M,@T*("`@("`@?2P-"B`@("`@(")I9"(Z(#(P+`T*("`@("`@(FEN=&5R=F%L
M(CH@(C%M(BP-"B`@("`@(")O<'1I;VYS(CH@>PT*("`@("`@("`B8V5L;$AE
M:6=H="(Z(")S;2(L#0H@("`@("`@(")F;V]T97(B.B![#0H@("`@("`@("`@
M(F-O=6YT4F]W<R(Z(&9A;'-E+`T*("`@("`@("`@(")F:65L9',B.B`B(BP-
M"B`@("`@("`@("`B<F5D=6-E<B(Z(%L-"B`@("`@("`@("`@(")S=6TB#0H@
M("`@("`@("`@72P-"B`@("`@("`@("`B<VAO=R(Z('1R=64-"B`@("`@("`@
M?2P-"B`@("`@("`@(G-H;W=(96%D97(B.B!T<G5E+`T*("`@("`@("`B<V]R
M=$)Y(CH@6PT*("`@("`@("`@('L-"B`@("`@("`@("`@(")D97-C(CH@=')U
M92P-"B`@("`@("`@("`@(")D:7-P;&%Y3F%M92(Z(")-87@B#0H@("`@("`@
M("`@?0T*("`@("`@("!=#0H@("`@("!]+`T*("`@("`@(G!L=6=I;E9E<G-I
M;VXB.B`B,3`N-"XQ,2(L#0H@("`@("`B<F5P96%T1&ER96-T:6]N(CH@(G8B
M+`T*("`@("`@(G1A<F=E=',B.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B
M3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I
M8W,B+`T*("`@("`@("`@(")D871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@
M(G1Y<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R
M8V4B+`T*("`@("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@
M("`@("`@("`@(")G<F]U<$)Y(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S
M<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@
M("`@("`@("`@?2P-"B`@("`@("`@("`@(")R961U8V4B.B![#0H@("`@("`@
M("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP
M92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G=H97)E
M(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@
M("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?0T*("`@("`@
M("`@('TL#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*
M("`@("`@("`@(")Q=65R>2(Z(")#;VYT86EN97).971W;W)K5')A;G-M:71"
M>71E<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN
M?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S
M<&%C93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D
M/71O<W1R:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S
M=')I;F<H3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@3F%M97-P86-E(#T]
M(%PB)$YA;65S<&%C95PB7&Y\(&EN=F]K92!P<F]M7V1E;'1A*"E<;GP@97AT
M96YD(%9A;'5E/59A;'5E+S8P7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L
M=64I(&)Y(&)I;BA4:6UE<W1A;7`L(#8P,#`P;7,I+"!.86UE<W!A8V4L(%!O
M9%QN?"!E>'1E;F0@3F%M93UR97!L86-E7W)E9V5X*%!O9"P@7"(H+5MA+7HP
M+3E=>S5]*21<(BP@7")<(BE<;GP@<W5M;6%R:7IE(%9A;'5E/6%V9RA686QU
M92DL($-O=6YT/61C;W5N="A0;V0I(&)Y(&)I;BA4:6UE<W1A;7`L(#8P,#`P
M;7,I+"!.86UE<W!A8V4L($YA;65<;GP@<W5M;6%R:7IE(%!O9',];6%X*$-O
M=6YT*2P@4#4P/7!E<F-E;G1I;&4H5F%L=64L(#`N-2DL(%`Y.3UP97)C96YT
M:6QE*%9A;'5E+"`P+CDY*2P@36EN/6UI;BA686QU92DL($UA>#UM87@H5F%L
M=64I(&)Y($YA;65S<&%C92P@3F%M95QN?"!P<F]J96-T+6%W87D@3F%M97-P
M86-E(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@
M("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B
M.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")7;W)K:6YG4V5T(BP-"B`@
M("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1A8FQE(@T*("`@("`@("!]#0H@
M("`@("!=+`T*("`@("`@(G1Y<&4B.B`B=&%B;&4B#0H@("`@?2P-"B`@("![
M#0H@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@(G1Y<&4B.B`B9W)A
M9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@
M("`B=6ED(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@('TL#0H@("`@
M("`B9FEE;&1#;VYF:6<B.B![#0H@("`@("`@(")D969A=6QT<R(Z('L-"B`@
M("`@("`@("`B8V]L;W(B.B![#0H@("`@("`@("`@("`B;6]D92(Z(")P86QE
M='1E+6-L87-S:6,B#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B8W5S=&]M
M(CH@>PT*("`@("`@("`@("`@(F%X:7-";W)D97)3:&]W(CH@9F%L<V4L#0H@
M("`@("`@("`@("`B87AI<T-E;G1E<F5D6F5R;R(Z(&9A;'-E+`T*("`@("`@
M("`@("`@(F%X:7-#;VQO<DUO9&4B.B`B=&5X="(L#0H@("`@("`@("`@("`B
M87AI<TQA8F5L(CH@(B(L#0H@("`@("`@("`@("`B87AI<U!L86-E;65N="(Z
M(")A=71O(BP-"B`@("`@("`@("`@(")B87)!;&EG;FUE;G0B.B`P+`T*("`@
M("`@("`@("`@(F1R87=3='EL92(Z(")L:6YE(BP-"B`@("`@("`@("`@(")F
M:6QL3W!A8VET>2(Z(#$P+`T*("`@("`@("`@("`@(F=R861I96YT36]D92(Z
M(")N;VYE(BP-"B`@("`@("`@("`@(")H:61E1G)O;2(Z('L-"B`@("`@("`@
M("`@("`@(FQE9V5N9"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=&]O;'1I
M<"(Z(&9A;'-E+`T*("`@("`@("`@("`@("`B=FEZ(CH@9F%L<V4-"B`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`B:6YS97)T3G5L;',B.B!F86QS92P-
M"B`@("`@("`@("`@(")L:6YE26YT97)P;VQA=&EO;B(Z(")L:6YE87(B+`T*
M("`@("`@("`@("`@(FQI;F57:61T:"(Z(#$L#0H@("`@("`@("`@("`B<&]I
M;G13:7IE(CH@-2P-"B`@("`@("`@("`@(")S8V%L941I<W1R:6)U=&EO;B(Z
M('L-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B;&EN96%R(@T*("`@("`@("`@
M("`@?2P-"B`@("`@("`@("`@(")S:&]W4&]I;G1S(CH@(FYE=F5R(BP-"B`@
M("`@("`@("`@(")S<&%N3G5L;',B.B!F86QS92P-"B`@("`@("`@("`@(")S
M=&%C:VEN9R(Z('L-"B`@("`@("`@("`@("`@(F=R;W5P(CH@(D$B+`T*("`@
M("`@("`@("`@("`B;6]D92(Z(")N;VYE(@T*("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@(")T:')E<VAO;&1S4W1Y;&4B.B![#0H@("`@("`@("`@("`@
M(")M;V1E(CH@(F]F9B(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*
M("`@("`@("`@(")M87!P:6YG<R(Z(%M=+`T*("`@("`@("`@(")T:')E<VAO
M;&1S(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B86)S;VQU=&4B+`T*("`@
M("`@("`@("`@(G-T97!S(CH@6PT*("`@("`@("`@("`@("![#0H@("`@("`@
M("`@("`@("`@(F-O;&]R(CH@(F=R965N(BP-"B`@("`@("`@("`@("`@("`B
M=F%L=64B.B!N=6QL#0H@("`@("`@("`@("`@('TL#0H@("`@("`@("`@("`@
M('L-"B`@("`@("`@("`@("`@("`B8V]L;W(B.B`B<F5D(BP-"B`@("`@("`@
M("`@("`@("`B=F%L=64B.B`X,`T*("`@("`@("`@("`@("!]#0H@("`@("`@
M("`@("!=#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B=6YI="(Z(")P<',B
M#0H@("`@("`@('TL#0H@("`@("`@(")O=F5R<FED97,B.B!;70T*("`@("`@
M?2P-"B`@("`@(")G<FED4&]S(CH@>PT*("`@("`@("`B:"(Z(#DL#0H@("`@
M("`@(")W(CH@-BP-"B`@("`@("`@(G@B.B`P+`T*("`@("`@("`B>2(Z(#4R
M#0H@("`@("!]+`T*("`@("`@(FED(CH@-BP-"B`@("`@(")I;G1E<G9A;"(Z
M("(Q;2(L#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(FQE9V5N9"(Z
M('L-"B`@("`@("`@("`B8V%L8W,B.B!;72P-"B`@("`@("`@("`B9&ES<&QA
M>4UO9&4B.B`B;&ES="(L#0H@("`@("`@("`@(G!L86-E;65N="(Z(")B;W1T
M;VTB+`T*("`@("`@("`@(")S:&]W3&5G96YD(CH@=')U90T*("`@("`@("!]
M+`T*("`@("`@("`B=&]O;'1I<"(Z('L-"B`@("`@("`@("`B;6]D92(Z(")S
M:6YG;&4B+`T*("`@("`@("`@(")S;W)T(CH@(FYO;F4B#0H@("`@("`@('T-
M"B`@("`@('TL#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@
M("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B
M.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@
M("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M
M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C
M93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N
M(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@
M(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A
M;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-
M"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@
M("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=
M+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U
M+C`N,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<DYE='=O<FM2
M96-E:79E4&%C:V5T<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM
M97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT
M96YD($YA;65S<&%C93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E
M>'1E;F0@4&]D/71O<W1R:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT
M86EN97(]=&]S=')I;F<H3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@3F%M
M97-P86-E(#T](%PB)$YA;65S<&%C95PB7&Y\(&EN=F]K92!P<F]M7V1E;'1A
M*"E<;GP@97AT96YD(%9A;'5E/59A;'5E+S8P7&Y\('-U;6UA<FEZ92!686QU
M93UA=F<H5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A
M;"DL($YA;65S<&%C92P@4&]D7&Y\('-U;6UA<FEZ92!686QU93US=6TH5F%L
M=64I*BTQ(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"E<;GP@
M<')O:F5C="!4:6UE<W1A;7`L(%)E8VEE=F4]5F%L=65<;GP@;W)D97(@8GD@
M5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B
M<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@
M("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")296-E
M:79E($)Y=&5S(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?
M<V5R:65S(@T*("`@("`@("!]+`T*("`@("`@("![#0H@("`@("`@("`@(D]P
M96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S
M(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T
M>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E
M(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*
M("`@("`@("`@('TL#0H@("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@
M("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I
M;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@
M("`@("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@
M("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B
M.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z
M('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@
M("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@
M("!]+`T*("`@("`@("`@(")H:61E(CH@9F%L<V4L#0H@("`@("`@("`@(G!L
M=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R>2(Z(")#
M;VYT86EN97).971W;W)K5')A;G-M:71086-K971S5&]T86Q<;GP@=VAE<F4@
M)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T97(]/5PB
M)$-L=7-T97)<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG*$QA8F5L
M<RYN86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B96QS+G!O
M9"E<;GP@97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E;',N8V]N=&%I
M;F5R*5QN?"!W:&5R92!.86UE<W!A8V4@/3T@7"(D3F%M97-P86-E7")<;GP@
M:6YV;VME('!R;VU?9&5L=&$H*5QN?"!E>'1E;F0@5F%L=64]5F%L=64O-C!<
M;GP@<W5M;6%R:7IE(%9A;'5E/6%V9RA686QU92D@8GD@8FEN*%1I;65S=&%M
M<"P@)%]?=&EM94EN=&5R=F%L*2P@3F%M97-P86-E+"!0;V1<;GP@<W5M;6%R
M:7IE(%9A;'5E/7-U;2A686QU92D@8GD@8FEN*%1I;65S=&%M<"P@)%]?=&EM
M94EN=&5R=F%L*5QN?"!P<F]J96-T(%1I;65S=&%M<"P@5')A;G-M:70]5F%L
M=65<;GP@;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B
M<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@
M(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@
M(")R969)9"(Z(")!(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I
M;65?<V5R:65S(@T*("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1I=&QE
M(CH@(DYE='=O<FL@4&%C:V5T<R(L#0H@("`@("`B='EP92(Z(")T:6UE<V5R
M:65S(@T*("`@('TL#0H@("`@>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@
M("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD
M871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X
M='TB#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@
M("`B9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@
M("`@("`@(FUO9&4B.B`B<&%L971T92UC;&%S<VEC(@T*("`@("`@("`@('TL
M#0H@("`@("`@("`@(F-U<W1O;2(Z('L-"B`@("`@("`@("`@(")A>&ES0F]R
M9&5R4VAO=R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X:7-#96YT97)E9%IE
M<F\B.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES0V]L;W)-;V1E(CH@(G1E
M>'0B+`T*("`@("`@("`@("`@(F%X:7-,86)E;"(Z("(B+`T*("`@("`@("`@
M("`@(F%X:7-0;&%C96UE;G0B.B`B875T;R(L#0H@("`@("`@("`@("`B8F%R
M06QI9VYM96YT(CH@,"P-"B`@("`@("`@("`@(")D<F%W4W1Y;&4B.B`B;&EN
M92(L#0H@("`@("`@("`@("`B9FEL;$]P86-I='DB.B`Q,"P-"B`@("`@("`@
M("`@(")G<F%D:65N=$UO9&4B.B`B;F]N92(L#0H@("`@("`@("`@("`B:&ED
M949R;VTB.B![#0H@("`@("`@("`@("`@(")L96=E;F0B.B!F86QS92P-"B`@
M("`@("`@("`@("`@(G1O;VQT:7`B.B!F86QS92P-"B`@("`@("`@("`@("`@
M(G9I>B(Z(&9A;'-E#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(FEN
M<V5R=$YU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B;&EN94EN=&5R<&]L
M871I;VXB.B`B;&EN96%R(BP-"B`@("`@("`@("`@(")L:6YE5VED=&@B.B`Q
M+`T*("`@("`@("`@("`@(G!O:6YT4VEZ92(Z(#4L#0H@("`@("`@("`@("`B
M<V-A;&5$:7-T<FEB=71I;VXB.B![#0H@("`@("`@("`@("`@(")T>7!E(CH@
M(FQI;F5A<B(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<VAO=U!O
M:6YT<R(Z(")N979E<B(L#0H@("`@("`@("`@("`B<W!A;DYU;&QS(CH@9F%L
M<V4L#0H@("`@("`@("`@("`B<W1A8VMI;F<B.B![#0H@("`@("`@("`@("`@
M(")G<F]U<"(Z(")!(BP-"B`@("`@("`@("`@("`@(FUO9&4B.B`B;F]N92(-
M"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=&AR97-H;VQD<U-T>6QE
M(CH@>PT*("`@("`@("`@("`@("`B;6]D92(Z(")O9F8B#0H@("`@("`@("`@
M("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;72P-
M"B`@("`@("`@("`B=&AR97-H;VQD<R(Z('L-"B`@("`@("`@("`@(")M;V1E
M(CH@(F%B<V]L=71E(BP-"B`@("`@("`@("`@(")S=&5P<R(Z(%L-"B`@("`@
M("`@("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")G<F5E;B(L
M#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@;G5L;`T*("`@("`@("`@("`@
M("!]+`T*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R
M(CH@(G)E9"(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@.#`-"B`@("`@
M("`@("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@("`@
M("`@("`@(G5N:70B.B`B0G!S(@T*("`@("`@("!]+`T*("`@("`@("`B;W9E
M<G)I9&5S(CH@6UT-"B`@("`@('TL#0H@("`@("`B9W)I9%!O<R(Z('L-"B`@
M("`@("`@(F@B.B`Y+`T*("`@("`@("`B=R(Z(#8L#0H@("`@("`@(")X(CH@
M-BP-"B`@("`@("`@(GDB.B`U,@T*("`@("`@?2P-"B`@("`@(")I9"(Z(#$Q
M+`T*("`@("`@(FEN=&5R=F%L(CH@(C%M(BP-"B`@("`@(")O<'1I;VYS(CH@
M>PT*("`@("`@("`B;&5G96YD(CH@>PT*("`@("`@("`@(")C86QC<R(Z(%M=
M+`T*("`@("`@("`@(")D:7-P;&%Y36]D92(Z(")L:7-T(BP-"B`@("`@("`@
M("`B<&QA8V5M96YT(CH@(F)O='1O;2(L#0H@("`@("`@("`@(G-H;W=,96=E
M;F0B.B!T<G5E#0H@("`@("`@('TL#0H@("`@("`@(")T;V]L=&EP(CH@>PT*
M("`@("`@("`@(")M;V1E(CH@(G-I;F=L92(L#0H@("`@("`@("`@(G-O<G0B
M.B`B;F]N92(-"B`@("`@("`@?0T*("`@("`@?2P-"B`@("`@(")T87)G971S
M(CH@6PT*("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*
M("`@("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B
M9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M
M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@
M(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!"
M>2(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@
M("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@
M("`@("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO
M;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@
M("`@("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@
M("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@
M(F%N9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@
M(")P;'5G:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@("`@("`@("`B<75E<GDB
M.B`B0V]N=&%I;F5R3F5T=V]R:U)E8V5I=F5086-K971S1')O<'!E9%1O=&%L
M7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#
M;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT;W-T
M<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R:6YG
M*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H3&%B
M96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@3F%M97-P86-E(#T](%PB)$YA;65S
M<&%C95PB7&Y\(&EN=F]K92!P<F]M7V1E;'1A*"E<;GP@97AT96YD(%9A;'5E
M/59A;'5E+S8P7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I(&)Y(&)I
M;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL($YA;65S<&%C92P@4&]D
M7&Y\('-U;6UA<FEZ92!686QU93US=6TH5F%L=64I*BTQ(&)Y(&)I;BA4:6UE
M<W1A;7`L("1?7W1I;65);G1E<G9A;"E<;GP@<')O:F5C="!4:6UE<W1A;7`L
M(%)E8VEE=F4]5F%L=65<;GP@;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-
M"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B
M<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E
M+`T*("`@("`@("`@(")R969)9"(Z(")296-E:79E($)Y=&5S(BP-"B`@("`@
M("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?<V5R:65S(@T*("`@("`@("!]
M+`T*("`@("`@("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@
M("`@("`@(")D871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T
M87-O=7)C92(Z('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U
M:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@("`@("`@('TL#0H@("`@
M("`@("`@(F5X<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z
M('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@
M("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@
M("`@("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B
M.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@
M("`@?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@
M(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N
M9"(-"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")H
M:61E(CH@9F%L<V4L#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-2XP
M+C,B+`T*("`@("`@("`@(")Q=65R>2(Z(")#;VYT86EN97).971W;W)K5')A
M;G-M:71086-K971S1')O<'!E9%1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT
M97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<
M;GP@97AT96YD($YA;65S<&%C93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E
M*5QN?"!E>'1E;F0@4&]D/71O<W1R:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N
M9"!#;VYT86EN97(]=&]S=')I;F<H3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE
M<F4@3F%M97-P86-E(#T](%PB)$YA;65S<&%C95PB7&Y\(&EN=F]K92!P<F]M
M7V1E;'1A*"E<;GP@97AT96YD(%9A;'5E/59A;'5E+S8P7&Y\('-U;6UA<FEZ
M92!686QU93UA=F<H5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65)
M;G1E<G9A;"DL($YA;65S<&%C92P@4&]D7&Y\('-U;6UA<FEZ92!686QU93US
M=6TH5F%L=64I(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"E<
M;GP@<')O:F5C="!4:6UE<W1A;7`L(%1R86YS;6ET/59A;'5E7&Y\(&]R9&5R
M(&)Y(%1I;65S=&%M<"!A<V-<;B(L#0H@("`@("`@("`@(G%U97)Y4V]U<F-E
M(CH@(G)A=R(L#0H@("`@("`@("`@(G%U97)Y5'EP92(Z(")+44PB+`T*("`@
M("`@("`@(")R87=-;V1E(CH@=')U92P-"B`@("`@("`@("`B<F5F260B.B`B
M02(L#0H@("`@("`@("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-
M"B`@("`@("`@?0T*("`@("`@72P-"B`@("`@(")T:71L92(Z(").971W;W)K
M($1R;W!P960@4&%C:V5T<R(L#0H@("`@("`B='EP92(Z(")T:6UE<V5R:65S
M(@T*("`@('TL#0H@("`@>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@
M("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A
M<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB
M#0H@("`@("!]+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B
M9&5F875L=',B.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@
M("`@(FUO9&4B.B`B<&%L971T92UC;&%S<VEC(@T*("`@("`@("`@('TL#0H@
M("`@("`@("`@(F-U<W1O;2(Z('L-"B`@("`@("`@("`@(")A>&ES0F]R9&5R
M4VAO=R(Z(&9A;'-E+`T*("`@("`@("`@("`@(F%X:7-#96YT97)E9%IE<F\B
M.B!F86QS92P-"B`@("`@("`@("`@(")A>&ES0V]L;W)-;V1E(CH@(G1E>'0B
M+`T*("`@("`@("`@("`@(F%X:7-,86)E;"(Z("(B+`T*("`@("`@("`@("`@
M(F%X:7-0;&%C96UE;G0B.B`B875T;R(L#0H@("`@("`@("`@("`B8F%R06QI
M9VYM96YT(CH@,"P-"B`@("`@("`@("`@(")D<F%W4W1Y;&4B.B`B;&EN92(L
M#0H@("`@("`@("`@("`B9FEL;$]P86-I='DB.B`Q,"P-"B`@("`@("`@("`@
M(")G<F%D:65N=$UO9&4B.B`B;F]N92(L#0H@("`@("`@("`@("`B:&ED949R
M;VTB.B![#0H@("`@("`@("`@("`@(")L96=E;F0B.B!F86QS92P-"B`@("`@
M("`@("`@("`@(G1O;VQT:7`B.B!F86QS92P-"B`@("`@("`@("`@("`@(G9I
M>B(Z(&9A;'-E#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(FEN<V5R
M=$YU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B;&EN94EN=&5R<&]L871I
M;VXB.B`B;&EN96%R(BP-"B`@("`@("`@("`@(")L:6YE5VED=&@B.B`Q+`T*
M("`@("`@("`@("`@(G!O:6YT4VEZ92(Z(#4L#0H@("`@("`@("`@("`B<V-A
M;&5$:7-T<FEB=71I;VXB.B![#0H@("`@("`@("`@("`@(")T>7!E(CH@(FQI
M;F5A<B(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<VAO=U!O:6YT
M<R(Z(")N979E<B(L#0H@("`@("`@("`@("`B<W!A;DYU;&QS(CH@9F%L<V4L
M#0H@("`@("`@("`@("`B<W1A8VMI;F<B.B![#0H@("`@("`@("`@("`@(")G
M<F]U<"(Z(")!(BP-"B`@("`@("`@("`@("`@(FUO9&4B.B`B;F]N92(-"B`@
M("`@("`@("`@('TL#0H@("`@("`@("`@("`B=&AR97-H;VQD<U-T>6QE(CH@
M>PT*("`@("`@("`@("`@("`B;6]D92(Z(")O9F8B#0H@("`@("`@("`@("!]
M#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;72P-"B`@
M("`@("`@("`B=&AR97-H;VQD<R(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@
M(F%B<V]L=71E(BP-"B`@("`@("`@("`@(")S=&5P<R(Z(%L-"B`@("`@("`@
M("`@("`@>PT*("`@("`@("`@("`@("`@(")C;VQO<B(Z(")G<F5E;B(L#0H@
M("`@("`@("`@("`@("`@(G9A;'5E(CH@;G5L;`T*("`@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@
M(G)E9"(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@.#`-"B`@("`@("`@
M("`@("`@?0T*("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@("`@("`@
M("`@(G5N:70B.B`B;F]N92(-"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R
M:61E<R(Z(%M=#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@
M("`@(")H(CH@.2P-"B`@("`@("`@(G<B.B`V+`T*("`@("`@("`B>"(Z(#$R
M+`T*("`@("`@("`B>2(Z(#4R#0H@("`@("!]+`T*("`@("`@(FED(CH@,3`L
M#0H@("`@("`B:6YT97)V86PB.B`B,6TB+`T*("`@("`@(F]P=&EO;G,B.B![
M#0H@("`@("`@(")L96=E;F0B.B![#0H@("`@("`@("`@(F-A;&-S(CH@6UTL
M#0H@("`@("`@("`@(F1I<W!L87E-;V1E(CH@(FQI<W0B+`T*("`@("`@("`@
M(")P;&%C96UE;G0B.B`B8F]T=&]M(BP-"B`@("`@("`@("`B<VAO=TQE9V5N
M9"(Z('1R=64-"B`@("`@("`@?2P-"B`@("`@("`@(G1O;VQT:7`B.B![#0H@
M("`@("`@("`@(FUO9&4B.B`B<VEN9VQE(BP-"B`@("`@("`@("`B<V]R="(Z
M(")N;VYE(@T*("`@("`@("!]#0H@("`@("!]+`T*("`@("`@(G1A<F=E=',B
M.B!;#0H@("`@("`@('L-"B`@("`@("`@("`B3W!E;D%)(CH@9F%L<V4L#0H@
M("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I8W,B+`T*("`@("`@("`@(")D
M871A<V]U<F-E(CH@>PT*("`@("`@("`@("`@(G1Y<&4B.B`B9W)A9F%N82UA
M>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B+`T*("`@("`@("`@("`@
M(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@("`@?2P-"B`@
M("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@(")G<F]U<$)Y
M(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@
M("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@(")R961U8V4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N
M<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@
M("`@("!]+`T*("`@("`@("`@("`@(G=H97)E(CH@>PT*("`@("`@("`@("`@
M("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B
M86YD(@T*("`@("`@("`@("`@?0T*("`@("`@("`@('TL#0H@("`@("`@("`@
M(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@("`@(")Q=65R>2(Z
M(")#;VYT86EN97).971W;W)K4F5C96EV945R<F]R<U1O=&%L7&Y\('=H97)E
M("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W:&5R92!#;'5S=&5R/3U<
M(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C93UT;W-T<FEN9RA,86)E
M;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O<W1R:6YG*$QA8F5L<RYP
M;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I;F<H3&%B96QS+F-O;G1A
M:6YE<BE<;GP@=VAE<F4@3F%M97-P86-E(#T](%PB)$YA;65S<&%C95PB7&Y\
M(&EN=F]K92!P<F]M7V1E;'1A*"E<;GP@97AT96YD(%9A;'5E/59A;'5E+S8P
M7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I(&)Y(&)I;BA4:6UE<W1A
M;7`L("1?7W1I;65);G1E<G9A;"DL($YA;65S<&%C92P@4&]D7&Y\('-U;6UA
M<FEZ92!686QU93US=6TH5F%L=64I*BTQ(&)Y(&)I;BA4:6UE<W1A;7`L("1?
M7W1I;65);G1E<G9A;"E<;GP@<')O:F5C="!4:6UE<W1A;7`L(%)E8VEE=F4]
M5F%L=65<;GP@;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@
M("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E
M(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@
M("`@(")R969)9"(Z(")296-E:79E($)Y=&5S(BP-"B`@("`@("`@("`B<F5S
M=6QT1F]R;6%T(CH@(G1I;65?<V5R:65S(@T*("`@("`@("!]+`T*("`@("`@
M("![#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D
M871A8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z
M('L-"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE
M>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$
M871A<V]U<F-E.G1E>'1](@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X
M<')E<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@
M("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T
M>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D
M=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@
M("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@
M("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I
M;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@
M("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")H:61E(CH@9F%L
M<V4L#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@
M("`@("`@(")Q=65R>2(Z(")#;VYT86EN97).971W;W)K5')A;G-M:71%<G)O
M<G-4;W1A;%QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@
M=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\(&5X=&5N9"!.86UE<W!A
M8V4]=&]S=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@97AT96YD(%!O9#UT
M;W-T<FEN9RA,86)E;',N<&]D*5QN?"!E>'1E;F0@0V]N=&%I;F5R/71O<W1R
M:6YG*$QA8F5L<RYC;VYT86EN97(I7&Y\('=H97)E($YA;65S<&%C92`]/2!<
M(B1.86UE<W!A8V5<(EQN?"!I;G9O:V4@<')O;5]D96QT82@I7&Y\(&5X=&5N
M9"!686QU93U686QU92\V,%QN?"!S=6UM87)I>F4@5F%L=64]879G*%9A;'5E
M*2!B>2!B:6XH5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI+"!.86UE<W!A
M8V4L(%!O9%QN?"!S=6UM87)I>F4@5F%L=64]<W5M*%9A;'5E*2!B>2!B:6XH
M5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI7&Y\('!R;VIE8W0@5&EM97-T
M86UP+"!4<F%N<VUI=#U686QU95QN?"!O<F1E<B!B>2!4:6UE<W1A;7`@87-C
M7&XB+`T*("`@("`@("`@(")Q=65R>5-O=7)C92(Z(")R87<B+`T*("`@("`@
M("`@(")Q=65R>51Y<&4B.B`B2U%,(BP-"B`@("`@("`@("`B<F%W36]D92(Z
M('1R=64L#0H@("`@("`@("`@(G)E9DED(CH@(D$B+`T*("`@("`@("`@(")R
M97-U;'1&;W)M870B.B`B=&EM95]S97)I97,B#0H@("`@("`@('T-"B`@("`@
M(%TL#0H@("`@("`B=&ET;&4B.B`B3F5T=V]R:R!%<G)O<G,B+`T*("`@("`@
M(G1Y<&4B.B`B=&EM97-E<FEE<R(-"B`@("!]+`T*("`@('L-"B`@("`@(")C
M;VQL87!S960B.B!F86QS92P-"B`@("`@(")G<FED4&]S(CH@>PT*("`@("`@
M("`B:"(Z(#$L#0H@("`@("`@(")W(CH@,C0L#0H@("`@("`@(")X(CH@,"P-
M"B`@("`@("`@(GDB.B`V,0T*("`@("`@?2P-"B`@("`@(")I9"(Z(#$R+`T*
M("`@("`@(G!A;F5L<R(Z(%M=+`T*("`@("`@(G1I=&QE(CH@(D1I<VL@24\B
M+`T*("`@("`@(G1Y<&4B.B`B<F]W(@T*("`@('TL#0H@("`@>PT*("`@("`@
M(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G5I9"(Z
M("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("!]+`T*("`@("`@(F9I96QD
M0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B.B![#0H@("`@("`@("`@
M(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B.B`B<&%L971T92UC;&%S
M<VEC(@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F-U<W1O;2(Z('L-"B`@
M("`@("`@("`@(")A>&ES0F]R9&5R4VAO=R(Z(&9A;'-E+`T*("`@("`@("`@
M("`@(F%X:7-#96YT97)E9%IE<F\B.B!F86QS92P-"B`@("`@("`@("`@(")A
M>&ES0V]L;W)-;V1E(CH@(G1E>'0B+`T*("`@("`@("`@("`@(F%X:7-,86)E
M;"(Z("(B+`T*("`@("`@("`@("`@(F%X:7-0;&%C96UE;G0B.B`B875T;R(L
M#0H@("`@("`@("`@("`B8F%R06QI9VYM96YT(CH@,"P-"B`@("`@("`@("`@
M(")D<F%W4W1Y;&4B.B`B;&EN92(L#0H@("`@("`@("`@("`B9FEL;$]P86-I
M='DB.B`Q,"P-"B`@("`@("`@("`@(")G<F%D:65N=$UO9&4B.B`B;F]N92(L
M#0H@("`@("`@("`@("`B:&ED949R;VTB.B![#0H@("`@("`@("`@("`@(")L
M96=E;F0B.B!F86QS92P-"B`@("`@("`@("`@("`@(G1O;VQT:7`B.B!F86QS
M92P-"B`@("`@("`@("`@("`@(G9I>B(Z(&9A;'-E#0H@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@(FEN<V5R=$YU;&QS(CH@9F%L<V4L#0H@("`@("`@
M("`@("`B;&EN94EN=&5R<&]L871I;VXB.B`B;&EN96%R(BP-"B`@("`@("`@
M("`@(")L:6YE5VED=&@B.B`Q+`T*("`@("`@("`@("`@(G!O:6YT4VEZ92(Z
M(#4L#0H@("`@("`@("`@("`B<V-A;&5$:7-T<FEB=71I;VXB.B![#0H@("`@
M("`@("`@("`@(")T>7!E(CH@(FQI;F5A<B(-"B`@("`@("`@("`@('TL#0H@
M("`@("`@("`@("`B<VAO=U!O:6YT<R(Z(")N979E<B(L#0H@("`@("`@("`@
M("`B<W!A;DYU;&QS(CH@9F%L<V4L#0H@("`@("`@("`@("`B<W1A8VMI;F<B
M.B![#0H@("`@("`@("`@("`@(")G<F]U<"(Z(")!(BP-"B`@("`@("`@("`@
M("`@(FUO9&4B.B`B;F]N92(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`B=&AR97-H;VQD<U-T>6QE(CH@>PT*("`@("`@("`@("`@("`B;6]D92(Z
M(")O9F8B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@
M("`B;6%P<&EN9W,B.B!;72P-"B`@("`@("`@("`B=&AR97-H;VQD<R(Z('L-
M"B`@("`@("`@("`@(")M;V1E(CH@(F%B<V]L=71E(BP-"B`@("`@("`@("`@
M(")S=&5P<R(Z(%L-"B`@("`@("`@("`@("`@>PT*("`@("`@("`@("`@("`@
M(")C;VQO<B(Z(")G<F5E;B(L#0H@("`@("`@("`@("`@("`@(G9A;'5E(CH@
M;G5L;`T*("`@("`@("`@("`@("!]+`T*("`@("`@("`@("`@("![#0H@("`@
M("`@("`@("`@("`@(F-O;&]R(CH@(G)E9"(L#0H@("`@("`@("`@("`@("`@
M(G9A;'5E(CH@.#`-"B`@("`@("`@("`@("`@?0T*("`@("`@("`@("`@70T*
M("`@("`@("`@('TL#0H@("`@("`@("`@(G5N:70B.B`B8GET97,B#0H@("`@
M("`@('TL#0H@("`@("`@(")O=F5R<FED97,B.B!;70T*("`@("`@?2P-"B`@
M("`@(")G<FED4&]S(CH@>PT*("`@("`@("`B:"(Z(#$Q+`T*("`@("`@("`B
M=R(Z(#$R+`T*("`@("`@("`B>"(Z(#`L#0H@("`@("`@(")Y(CH@-C(-"B`@
M("`@('TL#0H@("`@("`B:60B.B`Q,RP-"B`@("`@(")I;G1E<G9A;"(Z("(Q
M;2(L#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@("`@(FQE9V5N9"(Z('L-
M"B`@("`@("`@("`B8V%L8W,B.B!;72P-"B`@("`@("`@("`B9&ES<&QA>4UO
M9&4B.B`B;&ES="(L#0H@("`@("`@("`@(G!L86-E;65N="(Z(")B;W1T;VTB
M+`T*("`@("`@("`@(")S:&]W3&5G96YD(CH@=')U90T*("`@("`@("!]+`T*
M("`@("`@("`B=&]O;'1I<"(Z('L-"B`@("`@("`@("`B;6]D92(Z(")S:6YG
M;&4B+`T*("`@("`@("`@(")S;W)T(CH@(FYO;F4B#0H@("`@("`@('T-"B`@
M("`@('TL#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@("`@("`@>PT*("`@("`@
M("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@("`B9&%T86)A<V4B.B`B
M365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@
M("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T
M87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@(B1[1&%T87-O=7)C93IT
M97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@
M>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E
M>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@
M("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@
M(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B
M=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*
M("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@
M("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N
M,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A:6YE<D9S4F5A9'-">71E
M<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!W
M:&5R92!#;'5S=&5R/3U<(B1#;'5S=&5R7")<;GP@97AT96YD($YA;65S<&%C
M93UT;W-T<FEN9RA,86)E;',N;F%M97-P86-E*5QN?"!E>'1E;F0@4&]D/71O
M<W1R:6YG*$QA8F5L<RYP;V0I7&Y\(&5X=&5N9"!#;VYT86EN97(]=&]S=')I
M;F<H3&%B96QS+F-O;G1A:6YE<BE<;GP@=VAE<F4@3F%M97-P86-E(#T](%PB
M)$YA;65S<&%C95PB7&Y\(&EN=F]K92!P<F]M7V1E;'1A*"E<;GP@97AT96YD
M(%9A;'5E/59A;'5E+S8P7&Y\('-U;6UA<FEZ92!686QU93UA=F<H5F%L=64I
M*BTQ(&)Y(&)I;BA4:6UE<W1A;7`L("1?7W1I;65);G1E<G9A;"DL('1O<W1R
M:6YG*$QA8F5L<RYI9"E<;GP@<')O:F5C="!4:6UE<W1A;7`L(%)E860]5F%L
M=65<;GP@;W)D97(@8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B
M<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@
M(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@
M(")R969)9"(Z(")296-E:79E($)Y=&5S(BP-"B`@("`@("`@("`B<F5S=6QT
M1F]R;6%T(CH@(G1I;65?<V5R:65S(@T*("`@("`@("!]+`T*("`@("`@("![
M#0H@("`@("`@("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")D871A
M8F%S92(Z(")-971R:6-S(BP-"B`@("`@("`@("`B9&%T87-O=7)C92(Z('L-
M"B`@("`@("`@("`@(")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L
M;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@("`@(")U:60B.B`B)'M$871A
M<V]U<F-E.G1E>'1](@T*("`@("`@("`@('TL#0H@("`@("`@("`@(F5X<')E
M<W-I;VXB.B![#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-"B`@("`@("`@
M("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E
M(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B<F5D=6-E
M(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;72P-"B`@("`@
M("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@?2P-"B`@("`@
M("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS
M(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@
M("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")H:61E(CH@9F%L<V4L
M#0H@("`@("`@("`@(G!L=6=I;E9E<G-I;VXB.B`B-2XP+C,B+`T*("`@("`@
M("`@(")Q=65R>2(Z(")#;VYT86EN97)&<U=R:71E<T)Y=&5S5&]T86Q<;GP@
M=VAE<F4@)%]?=&EM949I;'1E<BA4:6UE<W1A;7`I7&Y\('=H97)E($-L=7-T
M97(]/5PB)$-L=7-T97)<(EQN?"!E>'1E;F0@3F%M97-P86-E/71O<W1R:6YG
M*$QA8F5L<RYN86UE<W!A8V4I7&Y\(&5X=&5N9"!0;V0]=&]S=')I;F<H3&%B
M96QS+G!O9"E<;GP@97AT96YD($-O;G1A:6YE<CUT;W-T<FEN9RA,86)E;',N
M8V]N=&%I;F5R*5QN?"!W:&5R92!.86UE<W!A8V4@/3T@7"(D3F%M97-P86-E
M7")<;GP@:6YV;VME('!R;VU?9&5L=&$H*5QN?"!E>'1E;F0@5F%L=64]5F%L
M=64O-C!<;GP@<W5M;6%R:7IE(%9A;'5E/6%V9RA686QU92D@8GD@8FEN*%1I
M;65S=&%M<"P@)%]?=&EM94EN=&5R=F%L*2P@=&]S=')I;F<H3&%B96QS+FED
M*5QN?"!P<F]J96-T(%1I;65S=&%M<"P@5W)I=&4]5F%L=65<;GP@;W)D97(@
M8GD@5&EM97-T86UP(&%S8UQN(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B
M.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@
M("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")!
M(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1I;65?<V5R:65S(@T*
M("`@("`@("!]#0H@("`@("!=+`T*("`@("`@(G1I=&QE(CH@(D1I<VL@5&AR
M;W5G:'!U="(L#0H@("`@("`B='EP92(Z(")T:6UE<V5R:65S(@T*("`@('TL
M#0H@("`@>PT*("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@(")T>7!E
M(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-
M"B`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("!]
M+`T*("`@("`@(F9I96QD0V]N9FEG(CH@>PT*("`@("`@("`B9&5F875L=',B
M.B![#0H@("`@("`@("`@(F-O;&]R(CH@>PT*("`@("`@("`@("`@(FUO9&4B
M.B`B<&%L971T92UC;&%S<VEC(@T*("`@("`@("`@('TL#0H@("`@("`@("`@
M(F-U<W1O;2(Z('L-"B`@("`@("`@("`@(")A>&ES0F]R9&5R4VAO=R(Z(&9A
M;'-E+`T*("`@("`@("`@("`@(F%X:7-#96YT97)E9%IE<F\B.B!F86QS92P-
M"B`@("`@("`@("`@(")A>&ES0V]L;W)-;V1E(CH@(G1E>'0B+`T*("`@("`@
M("`@("`@(F%X:7-,86)E;"(Z("(B+`T*("`@("`@("`@("`@(F%X:7-0;&%C
M96UE;G0B.B`B875T;R(L#0H@("`@("`@("`@("`B8F%R06QI9VYM96YT(CH@
M,"P-"B`@("`@("`@("`@(")D<F%W4W1Y;&4B.B`B;&EN92(L#0H@("`@("`@
M("`@("`B9FEL;$]P86-I='DB.B`Q,"P-"B`@("`@("`@("`@(")G<F%D:65N
M=$UO9&4B.B`B;F]N92(L#0H@("`@("`@("`@("`B:&ED949R;VTB.B![#0H@
M("`@("`@("`@("`@(")L96=E;F0B.B!F86QS92P-"B`@("`@("`@("`@("`@
M(G1O;VQT:7`B.B!F86QS92P-"B`@("`@("`@("`@("`@(G9I>B(Z(&9A;'-E
M#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@(FEN<V5R=$YU;&QS(CH@
M9F%L<V4L#0H@("`@("`@("`@("`B;&EN94EN=&5R<&]L871I;VXB.B`B;&EN
M96%R(BP-"B`@("`@("`@("`@(")L:6YE5VED=&@B.B`Q+`T*("`@("`@("`@
M("`@(G!O:6YT4VEZ92(Z(#4L#0H@("`@("`@("`@("`B<V-A;&5$:7-T<FEB
M=71I;VXB.B![#0H@("`@("`@("`@("`@(")T>7!E(CH@(FQI;F5A<B(-"B`@
M("`@("`@("`@('TL#0H@("`@("`@("`@("`B<VAO=U!O:6YT<R(Z(")N979E
M<B(L#0H@("`@("`@("`@("`B<W!A;DYU;&QS(CH@9F%L<V4L#0H@("`@("`@
M("`@("`B<W1A8VMI;F<B.B![#0H@("`@("`@("`@("`@(")G<F]U<"(Z(")!
M(BP-"B`@("`@("`@("`@("`@(FUO9&4B.B`B;F]N92(-"B`@("`@("`@("`@
M('TL#0H@("`@("`@("`@("`B=&AR97-H;VQD<U-T>6QE(CH@>PT*("`@("`@
M("`@("`@("`B;6]D92(Z(")O9F8B#0H@("`@("`@("`@("!]#0H@("`@("`@
M("`@?2P-"B`@("`@("`@("`B;6%P<&EN9W,B.B!;72P-"B`@("`@("`@("`B
M=&AR97-H;VQD<R(Z('L-"B`@("`@("`@("`@(")M;V1E(CH@(F%B<V]L=71E
M(BP-"B`@("`@("`@("`@(")S=&5P<R(Z(%L-"B`@("`@("`@("`@("`@>PT*
M("`@("`@("`@("`@("`@(")C;VQO<B(Z(")G<F5E;B(L#0H@("`@("`@("`@
M("`@("`@(G9A;'5E(CH@;G5L;`T*("`@("`@("`@("`@("!]+`T*("`@("`@
M("`@("`@("![#0H@("`@("`@("`@("`@("`@(F-O;&]R(CH@(G)E9"(L#0H@
M("`@("`@("`@("`@("`@(G9A;'5E(CH@.#`-"B`@("`@("`@("`@("`@?0T*
M("`@("`@("`@("`@70T*("`@("`@("`@('TL#0H@("`@("`@("`@(G5N:70B
M.B`B:6]P<R(-"B`@("`@("`@?2P-"B`@("`@("`@(F]V97)R:61E<R(Z(%M=
M#0H@("`@("!]+`T*("`@("`@(F=R:610;W,B.B![#0H@("`@("`@(")H(CH@
M,3$L#0H@("`@("`@(")W(CH@,3(L#0H@("`@("`@(")X(CH@,3(L#0H@("`@
M("`@(")Y(CH@-C(-"B`@("`@('TL#0H@("`@("`B:60B.B`Q-"P-"B`@("`@
M(")I;G1E<G9A;"(Z("(Q;2(L#0H@("`@("`B;W!T:6]N<R(Z('L-"B`@("`@
M("`@(FQE9V5N9"(Z('L-"B`@("`@("`@("`B8V%L8W,B.B!;72P-"B`@("`@
M("`@("`B9&ES<&QA>4UO9&4B.B`B;&ES="(L#0H@("`@("`@("`@(G!L86-E
M;65N="(Z(")B;W1T;VTB+`T*("`@("`@("`@(")S:&]W3&5G96YD(CH@=')U
M90T*("`@("`@("!]+`T*("`@("`@("`B=&]O;'1I<"(Z('L-"B`@("`@("`@
M("`B;6]D92(Z(")S:6YG;&4B+`T*("`@("`@("`@(")S;W)T(CH@(FYO;F4B
M#0H@("`@("`@('T-"B`@("`@('TL#0H@("`@("`B=&%R9V5T<R(Z(%L-"B`@
M("`@("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@("`@
M("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S;W5R
M8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E+61A
M=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED(CH@
M(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@("`@
M(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![#0H@
M("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@
M("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@("`@
M(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL
M#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL
M#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E>'!R
M97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@
M("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B<&QU9VEN
M5F5R<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@("`@(G%U97)Y(CH@(D-O;G1A
M:6YE<D9S4F5A9'-4;W1A;%QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S
M=&%M<"E<;GP@=VAE<F4@0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\(&5X=&5N
M9"!.86UE<W!A8V4]=&]S=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@97AT
M96YD(%!O9#UT;W-T<FEN9RA,86)E;',N<&]D*5QN?"!E>'1E;F0@0V]N=&%I
M;F5R/71O<W1R:6YG*$QA8F5L<RYC;VYT86EN97(I7&Y\('=H97)E($YA;65S
M<&%C92`]/2!<(B1.86UE<W!A8V5<(EQN?"!I;G9O:V4@<')O;5]D96QT82@I
M7&Y\(&5X=&5N9"!686QU93U686QU92\V,%QN?"!S=6UM87)I>F4@5F%L=64]
M879G*%9A;'5E*2HM,2!B>2!B:6XH5&EM97-T86UP+"`D7U]T:6UE26YT97)V
M86PI+"!T;W-T<FEN9RA,86)E;',N:60I7&Y\('!R;VIE8W0@5&EM97-T86UP
M+"!296%D/59A;'5E7&Y\(&]R9&5R(&)Y(%1I;65S=&%M<"!A<V-<;B(L#0H@
M("`@("`@("`@(G%U97)Y4V]U<F-E(CH@(G)A=R(L#0H@("`@("`@("`@(G%U
M97)Y5'EP92(Z(")+44PB+`T*("`@("`@("`@(")R87=-;V1E(CH@=')U92P-
M"B`@("`@("`@("`B<F5F260B.B`B4F5C96EV92!">71E<R(L#0H@("`@("`@
M("`@(G)E<W5L=$9O<FUA="(Z(")T:6UE7W-E<FEE<R(-"B`@("`@("`@?2P-
M"B`@("`@("`@>PT*("`@("`@("`@(")/<&5N04DB.B!F86QS92P-"B`@("`@
M("`@("`B9&%T86)A<V4B.B`B365T<FEC<R(L#0H@("`@("`@("`@(F1A=&%S
M;W5R8V4B.B![#0H@("`@("`@("`@("`B='EP92(Z(")G<F%F86YA+6%Z=7)E
M+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@("`@("`@("`@("`B=6ED
M(CH@(B1[1&%T87-O=7)C93IT97AT?2(-"B`@("`@("`@("!]+`T*("`@("`@
M("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@(F=R;W5P0GDB.B![
M#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@
M("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]+`T*("`@("`@("`@
M("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@
M6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@
M('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@("`@("`@("`@(")E
M>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B
M#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B:&ED
M92(Z(&9A;'-E+`T*("`@("`@("`@(")P;'5G:6Y697)S:6]N(CH@(C4N,"XS
M(BP-"B`@("`@("`@("`B<75E<GDB.B`B0V]N=&%I;F5R1G-7<FET97-4;W1A
M;%QN?"!W:&5R92`D7U]T:6UE1FEL=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@
M0VQU<W1E<CT]7"(D0VQU<W1E<EPB7&Y\(&5X=&5N9"!.86UE<W!A8V4]=&]S
M=')I;F<H3&%B96QS+FYA;65S<&%C92E<;GP@97AT96YD(%!O9#UT;W-T<FEN
M9RA,86)E;',N<&]D*5QN?"!E>'1E;F0@0V]N=&%I;F5R/71O<W1R:6YG*$QA
M8F5L<RYC;VYT86EN97(I7&Y\('=H97)E($YA;65S<&%C92`]/2!<(B1.86UE
M<W!A8V5<(EQN?"!I;G9O:V4@<')O;5]D96QT82@I7&Y\(&5X=&5N9"!686QU
M93U686QU92\V,%QN?"!S=6UM87)I>F4@5F%L=64]879G*%9A;'5E*2!B>2!B
M:6XH5&EM97-T86UP+"`D7U]T:6UE26YT97)V86PI+"!T;W-T<FEN9RA,86)E
M;',N:60I7&Y\('!R;VIE8W0@5&EM97-T86UP+"!4<F%N<VUI=#U686QU95QN
M?"!O<F1E<B!B>2!4:6UE<W1A;7`@87-C7&XB+`T*("`@("`@("`@(")Q=65R
M>5-O=7)C92(Z(")R87<B+`T*("`@("`@("`@(")Q=65R>51Y<&4B.B`B2U%,
M(BP-"B`@("`@("`@("`B<F%W36]D92(Z('1R=64L#0H@("`@("`@("`@(G)E
M9DED(CH@(D$B+`T*("`@("`@("`@(")R97-U;'1&;W)M870B.B`B=&EM95]S
M97)I97,B#0H@("`@("`@('T-"B`@("`@(%TL#0H@("`@("`B=&ET;&4B.B`B
M1&ES:R!)3U!3(BP-"B`@("`@(")T>7!E(CH@(G1I;65S97)I97,B#0H@("`@
M?0T*("!=+`T*("`B<F5F<F5S:"(Z("(Q;2(L#0H@(")S8VAE;6%697)S:6]N
M(CH@,SDL#0H@(")T86=S(CH@6UTL#0H@(")T96UP;&%T:6YG(CH@>PT*("`@
M(")L:7-T(CH@6PT*("`@("`@>PT*("`@("`@("`B:&ED92(Z(#`L#0H@("`@
M("`@(")I;F-L=61E06QL(CH@9F%L<V4L#0H@("`@("`@(")M=6QT:2(Z(&9A
M;'-E+`T*("`@("`@("`B;F%M92(Z(")$871A<V]U<F-E(BP-"B`@("`@("`@
M(F]P=&EO;G,B.B!;72P-"B`@("`@("`@(G%U97)Y(CH@(F=R869A;F$M87IU
M<F4M9&%T82UE>'!L;W)E<BUD871A<V]U<F-E(BP-"B`@("`@("`@(G%U97)Y
M5F%L=64B.B`B(BP-"B`@("`@("`@(G)E9G)E<V@B.B`Q+`T*("`@("`@("`B
M<F5G97@B.B`B(BP-"B`@("`@("`@(G-K:7!5<FQ3>6YC(CH@9F%L<V4L#0H@
M("`@("`@(")T>7!E(CH@(F1A=&%S;W5R8V4B#0H@("`@("!]+`T*("`@("`@
M>PT*("`@("`@("`B9&%T87-O=7)C92(Z('L-"B`@("`@("`@("`B='EP92(Z
M(")G<F%F86YA+6%Z=7)E+61A=&$M97AP;&]R97(M9&%T87-O=7)C92(L#0H@
M("`@("`@("`@(G5I9"(Z("(D>T1A=&%S;W5R8V4Z=&5X='TB#0H@("`@("`@
M('TL#0H@("`@("`@(")D969I;FET:6]N(CH@(D-O;G1A:6YE<D-P=55S86=E
M4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP
M*5QN?"!D:7-T:6YC="!#;'5S=&5R(BP-"B`@("`@("`@(FAI9&4B.B`P+`T*
M("`@("`@("`B:6YC;'5D94%L;"(Z(&9A;'-E+`T*("`@("`@("`B;75L=&DB
M.B!F86QS92P-"B`@("`@("`@(FYA;64B.B`B0VQU<W1E<B(L#0H@("`@("`@
M(")O<'1I;VYS(CH@6UTL#0H@("`@("`@(")Q=65R>2(Z('L-"B`@("`@("`@
M("`B3W!E;D%)(CH@9F%L<V4L#0H@("`@("`@("`@(F%Z=7)E3&]G06YA;'ET
M:6-S(CH@>PT*("`@("`@("`@("`@(G%U97)Y(CH@(B(L#0H@("`@("`@("`@
M("`B<F5S;W5R8V5S(CH@6UT-"B`@("`@("`@("!]+`T*("`@("`@("`@(")C
M;'5S=&5R57)I(CH@(B(L#0H@("`@("`@("`@(F1A=&%B87-E(CH@(DUE=')I
M8W,B+`T*("`@("`@("`@(")E>'!R97-S:6]N(CH@>PT*("`@("`@("`@("`@
M(F=R;W5P0GDB.B![#0H@("`@("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=
M+`T*("`@("`@("`@("`@("`B='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]
M+`T*("`@("`@("`@("`@(G)E9'5C92(Z('L-"B`@("`@("`@("`@("`@(F5X
M<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-
M"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B=VAE<F4B.B![#0H@("`@
M("`@("`@("`@(")E>'!R97-S:6]N<R(Z(%M=+`T*("`@("`@("`@("`@("`B
M='EP92(Z(")A;F0B#0H@("`@("`@("`@("!]#0H@("`@("`@("`@?2P-"B`@
M("`@("`@("`B<&QU9VEN5F5R<VEO;B(Z("(U+C`N,R(L#0H@("`@("`@("`@
M(G%U97)Y(CH@(D-O;G1A:6YE<D-P=55S86=E4V5C;VYD<U1O=&%L7&Y\('=H
M97)E("1?7W1I;65&:6QT97(H5&EM97-T86UP*5QN?"!D:7-T:6YC="!#;'5S
M=&5R(BP-"B`@("`@("`@("`B<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@
M("`@("`B<75E<GE4>7!E(CH@(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B
M.B!T<G5E+`T*("`@("`@("`@(")R969)9"(Z(")!(BP-"B`@("`@("`@("`B
M<F5S=6QT1F]R;6%T(CH@(G1A8FQE(BP-"B`@("`@("`@("`B<W5B<V-R:7!T
M:6]N(CH@(C)&-#5&-4(P+3=%13(M-$4Y,"U!-#$V+40S.#@Q0D1#1C<X,R(-
M"B`@("`@("`@?2P-"B`@("`@("`@(G)E9G)E<V@B.B`R+`T*("`@("`@("`B
M<F5G97@B.B`B(BP-"B`@("`@("`@(G-K:7!5<FQ3>6YC(CH@9F%L<V4L#0H@
M("`@("`@(")S;W)T(CH@,"P-"B`@("`@("`@(G1Y<&4B.B`B<75E<GDB#0H@
M("`@("!]+`T*("`@("`@>PT*("`@("`@("`B8W5R<F5N="(Z('L-"B`@("`@
M("`@("`B<V5L96-T960B.B!F86QS92P-"B`@("`@("`@("`B=&5X="(Z(")A
M9'@M;6]N(BP-"B`@("`@("`@("`B=F%L=64B.B`B861X+6UO;B(-"B`@("`@
M("`@?2P-"B`@("`@("`@(F1A=&%S;W5R8V4B.B![#0H@("`@("`@("`@(G1Y
M<&4B.B`B9W)A9F%N82UA>G5R92UD871A+65X<&QO<F5R+61A=&%S;W5R8V4B
M+`T*("`@("`@("`@(")U:60B.B`B)'M$871A<V]U<F-E.G1E>'1](@T*("`@
M("`@("!]+`T*("`@("`@("`B9&5F:6YI=&EO;B(Z(")#;VYT86EN97)#<'55
M<V5R4V5C;VYD<U1O=&%L7&Y\('=H97)E("1?7W1I;65&:6QT97(H5&EM97-T
M86UP*5QN?"!W:&5R92!#;'5S=&5R(#T](%PB)$-L=7-T97)<(EQN?"!W:&5R
M92!,86)E;',N;F%M97-P86-E("$](%PB7")<;GP@9&ES=&EN8W0@=&]S=')I
M;F<H3&%B96QS+FYA;65S<&%C92DB+`T*("`@("`@("`B:&ED92(Z(#`L#0H@
M("`@("`@(")I;F-L=61E06QL(CH@9F%L<V4L#0H@("`@("`@(")M=6QT:2(Z
M(&9A;'-E+`T*("`@("`@("`B;F%M92(Z(").86UE<W!A8V4B+`T*("`@("`@
M("`B;W!T:6]N<R(Z(%M=+`T*("`@("`@("`B<75E<GDB.B![#0H@("`@("`@
M("`@(D]P96Y!22(Z(&9A;'-E+`T*("`@("`@("`@(")A>G5R94QO9T%N86QY
M=&EC<R(Z('L-"B`@("`@("`@("`@(")Q=65R>2(Z("(B+`T*("`@("`@("`@
M("`@(G)E<V]U<F-E<R(Z(%M=#0H@("`@("`@("`@?2P-"B`@("`@("`@("`B
M8VQU<W1E<E5R:2(Z("(B+`T*("`@("`@("`@(")D871A8F%S92(Z(")-971R
M:6-S(BP-"B`@("`@("`@("`B97AP<F5S<VEO;B(Z('L-"B`@("`@("`@("`@
M(")F<F]M(CH@>PT*("`@("`@("`@("`@("`B<')O<&5R='DB.B![#0H@("`@
M("`@("`@("`@("`@(FYA;64B.B`B0V]N=&%I;F5R0W!U57-E<E-E8V]N9'-4
M;W1A;"(L#0H@("`@("`@("`@("`@("`@(G1Y<&4B.B`B<W1R:6YG(@T*("`@
M("`@("`@("`@("!]+`T*("`@("`@("`@("`@("`B='EP92(Z(")P<F]P97)T
M>2(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@("`B9W)O=7!">2(Z('L-
M"B`@("`@("`@("`@("`@(F5X<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@
M("`@(")T>7!E(CH@(F%N9"(-"B`@("`@("`@("`@('TL#0H@("`@("`@("`@
M("`B<F5D=6-E(CH@>PT*("`@("`@("`@("`@("`B97AP<F5S<VEO;G,B.B!;
M72P-"B`@("`@("`@("`@("`@(G1Y<&4B.B`B86YD(@T*("`@("`@("`@("`@
M?2P-"B`@("`@("`@("`@(")W:&5R92(Z('L-"B`@("`@("`@("`@("`@(F5X
M<')E<W-I;VYS(CH@6UTL#0H@("`@("`@("`@("`@(")T>7!E(CH@(F%N9"(-
M"B`@("`@("`@("`@('T-"B`@("`@("`@("!]+`T*("`@("`@("`@(")P;'5G
M:6Y697)S:6]N(CH@(C4N,"XS(BP-"B`@("`@("`@("`B<75E<GDB.B`B0V]N
M=&%I;F5R0W!U57-E<E-E8V]N9'-4;W1A;%QN?"!W:&5R92`D7U]T:6UE1FEL
M=&5R*%1I;65S=&%M<"E<;GP@=VAE<F4@0VQU<W1E<B`]/2!<(B1#;'5S=&5R
M7")<;GP@=VAE<F4@3&%B96QS+FYA;65S<&%C92`A/2!<(EPB7&Y\(&1I<W1I
M;F-T('1O<W1R:6YG*$QA8F5L<RYN86UE<W!A8V4I(BP-"B`@("`@("`@("`B
M<75E<GE3;W5R8V4B.B`B<F%W(BP-"B`@("`@("`@("`B<75E<GE4>7!E(CH@
M(DM13"(L#0H@("`@("`@("`@(G)A=TUO9&4B.B!T<G5E+`T*("`@("`@("`@
M(")R969)9"(Z(")!(BP-"B`@("`@("`@("`B<F5S=6QT1F]R;6%T(CH@(G1A
M8FQE(BP-"B`@("`@("`@("`B<W5B<V-R:7!T:6]N(CH@(C)&-#5&-4(P+3=%
M13(M-$4Y,"U!-#$V+40S.#@Q0D1#1C<X,R(-"B`@("`@("`@?2P-"B`@("`@
M("`@(G)E9G)E<V@B.B`R+`T*("`@("`@("`B<F5G97@B.B`B(BP-"B`@("`@
M("`@(G-K:7!5<FQ3>6YC(CH@9F%L<V4L#0H@("`@("`@(")S;W)T(CH@,2P-
M"B`@("`@("`@(G1Y<&4B.B`B<75E<GDB#0H@("`@("!]#0H@("`@70T*("!]
M+`T*("`B=&EM92(Z('L-"B`@("`B9G)O;2(Z(")N;W<M,6@B+`T*("`@(")T
M;R(Z(")N;W<B#0H@('TL#0H@(")T:6UE<&EC:V5R(CH@>PT*("`@(")N;W=$
M96QA>2(Z("(B#0H@('TL#0H@(")T:6UE>F]N92(Z(")U=&,B+`T*("`B=&ET
H;&4B.B`B3F%M97-P86-E<R(L#0H@(")W965K4W1A<G0B.B`B(@T*?2`B
`
end
SHAR_EOF
  (set 20 24 12 06 20 59 58 'dashboards/namespaces.json'
   eval "${shar_touch}") && \
  chmod 0644 'dashboards/namespaces.json'
if test $? -ne 0
then ${echo} "restore of dashboards/namespaces.json failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'dashboards/namespaces.json': 'MD5 check failed'
       ) << \SHAR_EOF
c49eadadb64cf77ffeefc17b37b4e325  dashboards/namespaces.json
SHAR_EOF

else
test `LC_ALL=C wc -c < 'dashboards/namespaces.json'` -ne 70780 && \
  ${echo} "restoration warning:  size of 'dashboards/namespaces.json' is not 70780"
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
  (set 20 24 11 12 17 26 14 'ingestor.yaml'
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
M8V4M9W)A<&@@:W5S=&\[(&1O"B`@("!I9B`A(&%Z(&5X=&5N<VEO;B!S:&]W
M("TM;F%M92`D15A4("8^("]D978O;G5L;#L@=&AE;@H@("`@("`@(')E860@
M+7`@(E1H92`G)&5X="<@97AT96YS:6]N(&ES(&YO="!I;G-T86QL960N($1O
M('EO=2!W86YT('1O(&EN<W1A;&P@:70@;F]W/R`H>2]N*2`B($E.4U1!3$Q?
M15A4"B`@("`@("`@:68@6UL@(B1)3E-404Q,7T585"(@/3T@(GDB(%U=.R!T
M:&5N"B`@("`@("`@("`@(&%Z(&5X=&5N<VEO;B!A9&0@+2UN86UE("(D15A4
M(@H@("`@("`@(&5L<V4*("`@("`@("`@("`@96-H;R`B5&AE("<D15A4)R!E
M>'1E;G-I;VX@:7,@<F5Q=6ER960N($5X:71I;F<N(@H@("`@("`@("`@("!E
M>&ET(#$*("`@("`@("!F:0H@("`@9FD*9&]N90H*(R!!<VL@9F]R('1H92!N
M86UE(&]F('1H92!A:W,@8VQU<W1E<B!A;F0@<F5A9"!I="!A<R!I;G!U="X@
M(%=I=&@@=&AA="!N86UE+"!R=6X@82!G<F%P:"!Q=65R>2!T;R!F:6YD"G)E
M860@+7`@(E!L96%S92!E;G1E<B!T:&4@;F%M92!O9B!T:&4@04M3(&-L=7-T
M97(@=VAE<F4@0418+4UO;B!C;VUP;VYE;G1S('-H;W5L9"!B92!D97!L;WEE
M9#H@(B!#3%535$52"G=H:6QE(%M;("UZ("(D>T-,55-415(O+R!](B!=73L@
M9&\*("`@(&5C:&\@(D-L=7-T97(@8V%N;F]T(&)E(&5M<'1Y+B!0;&5A<V4@
M96YT97(@=&AE(&YA;64@;V8@=&AE($%+4R!C;'5S=&5R.B(*("`@(')E860@
M0TQ54U1%4@ID;VYE"@HC(%)U;B!A(&=R87!H('%U97)Y('1O(&9I;F0@=&AE
M(&-L=7-T97(G<R!R97-O=7)C92!G<F]U<"!A;F0@<W5B<V-R:7!T:6]N(&ED
M"D-,55-415)?24Y&3STD*&%Z(&=R87!H('%U97)Y("UQ(")297-O=7)C97,@
M?"!W:&5R92!T>7!E(#U^("=-:6-R;W-O9G0N0V]N=&%I;F5R4V5R=FEC92]M
M86YA9V5D0VQU<W1E<G,G(&%N9"!N86UE(#U^("<D0TQ54U1%4B<@?"!P<F]J
M96-T(')E<V]U<F-E1W)O=7`L('-U8G-C<FEP=&EO;DED+"!L;V-A=&EO;B(I
M"FEF(%M;("0H96-H;R`D0TQ54U1%4E])3D9/('P@:G$@)RYD871A('P@;&5N
M9W1H)RD@+65Q(#`@75T[('1H96X*("`@(&5C:&\@(DYO($%+4R!C;'5S=&5R
M(&-O=6QD(&)E(&9O=6YD(&9O<B!T:&4@8VQU<W1E<B!N86UE("<D0TQ54U1%
M4B<N($5X:71I;F<N(@H@("`@97AI="`Q"F9I"@I215-/55)#15]'4D]54#TD
M*&5C:&\@)$-,55-415)?24Y&3R!\(&IQ("UR("<N9&%T85LP72YR97-O=7)C
M94=R;W5P)RD*4U5"4T-225!424].7TE$/20H96-H;R`D0TQ54U1%4E])3D9/
M('P@:G$@+7(@)RYD871A6S!=+G-U8G-C<FEP=&EO;DED)RD*4D5'24]./20H
M96-H;R`D0TQ54U1%4E])3D9/('P@:G$@+7(@)RYD871A6S!=+FQO8V%T:6]N
M)RD*"B,@1FEN9"!T:&4@;6%N86=E9"!I9&5N=&ET>2!C;&EE;G0@240@871T
M86-H960@=&\@=&AE($%+4R!N;V1E('!O;VQS"DY/1$5?4$]/3%])1$5.5$E4
M63TD*&%Z(&%K<R!S:&]W("TM<F5S;W5R8V4M9W)O=7`@)%)%4T]54D-%7T=2
M3U50("TM;F%M92`D0TQ54U1%4B`M+7%U97)Y(&ED96YT:71Y4')O9FEL92YK
M=6)E;&5T:61E;G1I='DN8VQI96YT260@+6\@:G-O;B!\(&IQ("X@+7(I"@IE
M8VAO"F5C:&\@+64@(D9O=6YD($%+4R!C;'5S=&5R(&EN9F\Z(@IE8VAO("UE
M("(@($%+4R!#;'5S=&5R($YA;64Z(%QE6S,R;21#3%535$527&5;,&TB"F5C
M:&\@+64@(B`@4F5S;W5R8V4@1W)O=7`Z(%QE6S,R;21215-/55)#15]'4D]5
M4%QE6S!M(@IE8VAO("UE("(@(%-U8G-C<FEP=&EO;B!)1#I<95LS,FT@)%-5
M0E-#4DE05$E/3E])1%QE6S!M(@IE8VAO("UE("(@(%)E9VEO;CH@7&5;,S)M
M)%)%1TE/3EQE6S!M(@IE8VAO("UE("(@($UA;F%G960@261E;G1I='D@0VQI
M96YT($E$.B!<95LS,FTD3D]$15]03T],7TE$14Y425197&5;,&TB"F5C:&\*
M<F5A9"`M<"`B27,@=&AI<R!I;F9O<FUA=&EO;B!C;W)R96-T/R`H>2]N*2`B
M($-/3D9)4DT*:68@6UL@(B1#3TY&25)-(B`A/2`B>2(@75T[('1H96X*("`@
M(&5C:&\@(D5X:71I;F<@87,@=&AE(&EN9F]R;6%T:6]N(&ES(&YO="!C;W)R
M96-T+B(*("`@(&5X:70@,0IF:0H*87H@86MS(&=E="UC<F5D96YT:6%L<R`M
M+7-U8G-C<FEP=&EO;B`D4U5"4T-225!424].7TE$("TM<F5S;W5R8V4M9W)O
M=7`@)%)%4T]54D-%7T=23U50("TM;F%M92`D0TQ54U1%4@H*96-H;PIR96%D
M("UP(")0;&5A<V4@96YT97(@=&AE($%Z=7)E($1A=&$@17AP;&]R97(@8VQU
M<W1E<B!N86UE('=H97)E($%$6"U-;VX@=VEL;"!S=&]R92!T96QE;65T<GDZ
M("(@0TQ54U1%4E].04U%"G=H:6QE(%M;("UZ("(D>T-,55-415)?3D%-12\O
M('TB(%U=.R!D;PH@("`@96-H;R`B0418(&-L=7-T97(@;F%M92!C86YN;W0@
M8F4@96UP='DN(%!L96%S92!E;G1E<B!T:&4@9&%T86)A<V4@;F%M93HB"B`@
M("!R96%D($-,55-415)?3D%-10ID;VYE"@I#3%535$527TE.1D\])"AA>B!G
M<F%P:"!Q=65R>2`M<2`B4F5S;W5R8V5S('P@=VAE<F4@='EP92`]?B`G36EC
M<F]S;V9T+DMU<W1O+V-L=7-T97)S)R!A;F0@;F%M92`]?B`G)$-,55-415)?
M3D%-12<@?"!P<F]J96-T(&YA;64L(')E<V]U<F-E1W)O=7`L('-U8G-C<FEP
M=&EO;DED+"!L;V-A=&EO;BP@<')O<&5R=&EE<RYU<FDB*0I#3%535$527T-/
M54Y4/20H96-H;R`D0TQ54U1%4E])3D9/('P@:G$@)RYD871A('P@;&5N9W1H
M)RD*2U535$]?4D5'24]./20H96-H;R`D0TQ54U1%4E])3D9/('P@:G$@+7(@
M)RYD871A6S!=+FQO8V%T:6]N)RD*"FEF(%M;("1#3%535$527T-/54Y4("UE
M<2`P(%U=.R!T:&5N"B`@("!E8VAO(").;R!+=7-T;R!C;'5S=&5R(&-O=6QD
M(&)E(&9O=6YD(&9O<B!T:&4@9&%T86)A<V4@;F%M92`G)$-,55-415)?3D%-
M12<N($5X:71I;F<N(@H@("`@97AI="`Q"F5L<V4*("`@($-,55-415)?3D%-
M13TD*&5C:&\@)$-,55-415)?24Y&3R!\(&IQ("UR("<N9&%T85LP72YN86UE
M)RD*("`@(%-50E-#4DE05$E/3E])1#TD*&5C:&\@)$-,55-415)?24Y&3R!\
M(&IQ("UR("<N9&%T85LP72YS=6)S8W)I<'1I;VY)9"<I"B`@("!215-/55)#
M15]'4D]54#TD*&5C:&\@)$-,55-415)?24Y&3R!\(&IQ("UR("<N9&%T85LP
M72YR97-O=7)C94=R;W5P)RD*("`@($%$6%]&441./20H96-H;R`D0TQ54U1%
M4E])3D9/('P@:G$@+7(@)RYD871A6S!=+G!R;W!E<G1I97-?=7)I)RD*9FD*
M96-H;PIE8VAO(")&;W5N9"!!1%@@8VQU<W1E<B!I;F9O.B(*96-H;R`M92`B
M("!#;'5S=&5R($YA;64Z(%QE6S,R;21#3%535$527TY!345<95LP;2(*96-H
M;R`M92`B("!3=6)S8W)I<'1I;VX@240Z(%QE6S,R;21354)30U))4%1)3TY?
M241<95LP;2(*96-H;R`M92`B("!297-O=7)C92!'<F]U<#H@7&5;,S)M)%)%
M4T]54D-%7T=23U507&5;,&TB"F5C:&\@+64@(B`@0418($911$XZ(%QE6S,R
M;21!1%A?1E%$3EQE6S!M(@IE8VAO("UE("(@(%)E9VEO;CH@7&5;,S)M)$M5
M4U1/7U)%1TE/3EQE6S!M(@IE8VAO"G)E860@+7`@(DES('1H:7,@=&AE(&-O
M<G)E8W0@0418(&-L=7-T97(@:6YF;S\@*'DO;BD@(B!#3TY&25)-"FEF(%M;
M("(D0T].1DE232(@(3T@(GDB(%U=.R!T:&5N"B`@("!E8VAO(")%>&ET:6YG
M(&%S('1H92!!1%@@8VQU<W1E<B!I;F9O(&ES(&YO="!C;W)R96-T+B(*("`@
M(&5X:70@,0IF:0H*9F]R($1!5$%"05-%7TY!344@:6X@365T<FEC<R!,;V=S
M.R!D;PH@("`@(R!#:&5C:R!I9B!T:&4@)$1!5$%"05-%7TY!344@9&%T86)A
M<V4@97AI<W1S"B`@("!$051!0D%315]%6$E35%,])"AA>B!K=7-T;R!D871A
M8F%S92!S:&]W("TM8VQU<W1E<BUN86UE("1#3%535$527TY!344@+2UR97-O
M=7)C92UG<F]U<"`D4D533U520T5?1U)/55`@+2UD871A8F%S92UN86UE("1$
M051!0D%315].04U%("TM<75E<GD@(FYA;64B("UO('1S=B`R/B]D978O;G5L
M;"!\?"!E8VAO("(B*0H@("`@:68@6UL@+7H@(B1$051!0D%315]%6$E35%,B
M(%U=.R!T:&5N"B`@("`@("`@96-H;R`B5&AE("1$051!0D%315].04U%(&1A
M=&%B87-E(&1O97,@;F]T(&5X:7-T+B!#<F5A=&EN9R!I="!N;W<N(@H@("`@
M("`@(&%Z(&MU<W1O(&1A=&%B87-E(&-R96%T92`M+6-L=7-T97(M;F%M92`D
M0TQ54U1%4E].04U%("TM<F5S;W5R8V4M9W)O=7`@)%)%4T]54D-%7T=23U50
M("TM9&%T86)A<V4M;F%M92`D1$%404)!4T5?3D%-12`M+7)E860M=W)I=&4M
M9&%T86)A<V4@('-O9G0M9&5L971E+7!E<FEO9#U0,S!$(&AO="UC86-H92UP
M97)I;V0]4#=$(&QO8V%T:6]N/21+55-43U]214=)3TX*("`@(&5L<V4*("`@
M("`@("!E8VAO(")4:&4@)$1!5$%"05-%7TY!344@9&%T86)A<V4@86QR96%D
M>2!E>&ES=',N(@H@("`@9FD*"B`@("`C($-H96-K(&EF('1H92!.3T1%7U!/
M3TQ?241%3E1)5%D@:7,@86X@861M:6X@;VX@=&AE("1$051!0D%315].04U%
M(&1A=&%B87-E"B`@("!!1$U)3E]#2$5#2STD*&%Z(&MU<W1O(&1A=&%B87-E
M(&QI<W0M<')I;F-I<&%L("TM8VQU<W1E<BUN86UE("1#3%535$527TY!344@
M+2UR97-O=7)C92UG<F]U<"`D4D533U520T5?1U)/55`@+2UD871A8F%S92UN
M86UE("1$051!0D%315].04U%("TM<75E<GD@(EL_='EP93T])T%P<"<@)B8@
M87!P260]/2<D3D]$15]03T],7TE$14Y42519)R`F)B!R;VQE/3TG061M:6XG
M72(@+6\@='-V*0H@("`@:68@6UL@+7H@(B1!1$U)3E]#2$5#2R(@75T[('1H
M96X*("`@("`@("!E8VAO(")4:&4@36%N86=E9"!)9&5N=&ET>2!#;&EE;G0@
M240@:7,@;F]T(&-O;F9I9W5R960@=&\@=7-E(&1A=&%B87-E("1$051!0D%3
M15].04U%+B!!9&1I;F<@:70@87,@86X@861M:6XN(@H@("`@("`@(&%Z(&MU
M<W1O(&1A=&%B87-E(&%D9"UP<FEN8VEP86P@+2UC;'5S=&5R+6YA;64@)$-,
M55-415)?3D%-12`M+7)E<V]U<F-E+6=R;W5P("1215-/55)#15]'4D]54"`M
M+61A=&%B87-E+6YA;64@)$1!5$%"05-%7TY!344@+2UV86QU92!R;VQE/4%D
M;6EN(&YA;64]041836]N('1Y<&4]87!P(&%P<"UI9#TD3D]$15]03T],7TE$
M14Y42519"B`@("!E;'-E"B`@("`@("`@96-H;R`B5&AE($UA;F%G960@261E
M;G1I='D@0VQI96YT($E$(&ES(&%L<F5A9'D@8V]N9FEG=7)E9"!T;R!U<V4@
M9&%T86)A<V4@)$1!5$%"05-%7TY!344N(@H@("`@9FD*9&]N90H*97AP;W)T
M($-,55-415(])$-,55-415(*97AP;W)T(%)%1TE/3CTD4D5'24]."F5X<&]R
M="!#3$E%3E1?240])$Y/1$5?4$]/3%])1$5.5$E460IE>'!O<G0@04187U52
M3#TD04187T911$X*96YV<W5B<W0@/"`D4T-225!47T1)4B]I;F=E<W1O<BYY
M86UL('P@:W5B96-T;"!A<'!L>2`M9B`M"F5N=G-U8G-T(#P@)%-#4DE05%]$
M25(O8V]L;&5C=&]R+GEA;6P@?"!K=6)E8W1L(&%P<&QY("UF("T*:W5B96-T
M;"!A<'!L>2`M9B`D4T-225!47T1)4B]K<VTN>6%M;`H*(R!297-T87)T(&%L
M;"!W;W)K;&]A9',@9F]R('-C:&5M82!C:&%N9V5S('1O(&)E(')E9FQE8W1E
M9"!I9B!T:&ES(&ES(&%N('5P9&%T90IK=6)E8W1L(')O;&QO=70@<F5S=&%R
M="!S=',@:6YG97-T;W(@+6X@861X+6UO;@IK=6)E8W1L(')O;&QO=70@<F5S
M=&%R="!D<R!C;VQL96-T;W(@+6X@861X+6UO;@IK=6)E8W1L(')O;&QO=70@
M<F5S=&%R="!D97!L;WD@8V]L;&5C=&]R+7-I;F=L971O;B`M;B!A9'@M;6]N
M"@I'4D%&04Y!7T5.1%!/24Y4/2(B"F5C:&\*<F5A9"`M<"`B1&\@>6]U('=A
M;G0@=&\@<V5T=7`@86X@07IU<F4@36%N86=E9"!'<F%F86YA(&EN<W1A;F-E
M('1O('9I<W5A;&EZ92!T:&4@04M3('1E;&5M971R>3\@*'DO;BD@(B!#3TY&
M25)-"FEF(%M;("(D0T].1DE232(@/3T@(GDB(%U=.R!T:&5N"B`@("!I9B`A
M(&%Z(&5X=&5N<VEO;B!S:&]W("TM;F%M92!A;6<@)CX@+V1E=B]N=6QL.R!T
M:&5N"B`@("`@("`@<F5A9"`M<"`B5&AE("=A;6<G(&5X=&5N<VEO;B!I<R!N
M;W0@:6YS=&%L;&5D+B!$;R!Y;W4@=V%N="!T;R!I;G-T86QL(&ET(&YO=S\@
M*'DO;BD@(B!)3E-404Q,7T585`H@("`@("`@(&EF(%M;("(D24Y35$%,3%]%
M6%0B(#T](")Y(B!=73L@=&AE;@H@("`@("`@("`@("!A>B!E>'1E;G-I;VX@
M861D("TM;F%M92`B86UG(@H@("`@("`@(&5L<V4*("`@("`@("`@("`@96-H
M;R`B5&AE("=A;6<G(&5X=&5N<VEO;B!I<R!R97%U:7)E9"!T;R!S971U<"!G
M<F%F86YA+B!%>&ET:6YG+B(*("`@("`@("`@("`@97AI="`Q"B`@("`@("`@
M9FD*("`@(&9I"@H@("`@<F5A9"`M<"`B4&QE87-E(&5N=&5R(&YA;64@;V8@
M07IU<F4@36%N86=E9"!'<F%F86YA(&EN<W1A;F-E("AN97<@;W(@97AI<W1I
M;F<I.B`B($=2049!3D$*("`@('=H:6QE(%M;("UZ("(D>T=2049!3D$O+R!]
M(B!=73L@9&\*("`@("`@("!E8VAO("));G-T86YC92!N86UE(&-A;FYO="!B
M92!E;7!T>2X@4&QE87-E(&5N=&5R('1H92!N86UE.B(*("`@("`@("!R96%D
M($=2049!3D$*("`@(&1O;F4*"B`@("!'4D%&04Y!7TE.1D\])"AA>B!G<F%P
M:"!Q=65R>2`M<2`B4F5S;W5R8V5S('P@=VAE<F4@='EP92`]?B`G36EC<F]S
M;V9T+D1A<VAB;V%R9"]G<F%F86YA)R!A;F0@;F%M92`]?B`G)$=2049!3D$G
M('P@<')O:F5C="!N86UE+"!R97-O=7)C94=R;W5P+"!S=6)S8W)I<'1I;VY)
M9"P@;&]C871I;VXL('!R;W!E<G1I97,N96YD<&]I;G0B*0H@("`@1U)!1D%.
M05]#3U5.5#TD*&5C:&\@)$=2049!3D%?24Y&3R!\(&IQ("<N9&%T82!\(&QE
M;F=T:"<I"B`@("!I9B!;6R!'4D%&04Y!7T-/54Y4("UE<2`P(%U=.R!T:&5N
M"B`@("`@("`@(R!#<F5A=&4@07IU<F4@36%N86=E9"!'<F%F86YA"B`@("`@
M("`@96-H;R`B5&AE("1'4D%&04Y!(&EN<W1A;F-E(&1O97,@;F]T(&5X:7-T
M+B!#<F5A=&EN9R!I="!I;B`D4D533U520T5?1U)/55`@<F5S;W5R8V4@9W)O
M=7`N(@H@("`@("`@(&%Z(&=R869A;F$@8W)E871E("TM;F%M92`B)$=2049!
M3D$B("TM<F5S;W5R8V4M9W)O=7`@(B1215-/55)#15]'4D]54"(*("`@("`@
M("!'4D%&04Y!7TE.1D\])"AA>B!G<F%F86YA('-H;W<@+2UN86UE("(D1U)!
M1D%.02(@+2UR97-O=7)C92UG<F]U<"`B)%)%4T]54D-%7T=23U50(B`M;R!J
M<V]N*0H@("`@("`@($=2049!3D%?4D<])%)%4T]54D-%7T=23U50"B`@("`@
M("`@1U)!1D%.05]%3D103TE.5#TD*&5C:&\@)$=2049!3D%?24Y&3R!\(&IQ
M("UR("<N<')O<&5R=&EE<RYE;F1P;VEN="<I"B`@("!E;'-E"B`@("`@("`@
M1U)!1D%.05]354(])"AE8VAO("1'4D%&04Y!7TE.1D\@?"!J<2`M<B`G+F1A
M=&%;,%TN<W5B<V-R:7!T:6]N260G*0H@("`@("`@($=2049!3D%?4D<])"AE
M8VAO("1'4D%&04Y!7TE.1D\@?"!J<2`M<B`G+F1A=&%;,%TN<F5S;W5R8V5'
M<F]U<"<I"B`@("`@("`@1U)!1D%.05]%3D103TE.5#TD*&5C:&\@)$=2049!
M3D%?24Y&3R!\(&IQ("UR("<N9&%T85LP72YP<F]P97)T:65S7V5N9'!O:6YT
M)RD*("`@("`@("!'4D%&04Y!7U)%1TE/3CTD*&5C:&\@)$=2049!3D%?24Y&
M3R!\(&IQ("UR("<N9&%T85LP72YL;V-A=&EO;B<I"@H@("`@("`@(&5C:&\*
M("`@("`@("!E8VAO(")&;W5N9"!'<F%F86YA(&EN<W1A;F-E.B(*("`@("`@
M("!E8VAO("UE("(@($YA;64Z(%QE6S,R;21'4D%&04Y!7&5;,&TB"B`@("`@
M("`@96-H;R`M92`B("!3=6)S8W)I<'1I;VX@240Z(%QE6S,R;21'4D%&04Y!
M7U-50EQE6S!M(@H@("`@("`@(&5C:&\@+64@(B`@4F5S;W5R8V4@1W)O=7`Z
M(%QE6S,R;21'4D%&04Y!7U)'7&5;,&TB"B`@("`@("`@96-H;R`M92`B("!%
M;F1P;VEN=#H@7&5;,S)M)$=2049!3D%?14Y$4$])3E1<95LP;2(*("`@("`@
M("!E8VAO("UE("(@(%)E9VEO;CH@7&5;,S)M)$=2049!3D%?4D5'24].7&5;
M,&TB"B`@("`@("`@96-H;PH@("`@("`@(')E860@+7`@(DES('1H:7,@=&AE
M(&-O<G)E8W0@:6YF;S\@*'DO;BD@(B!#3TY&25)-"B`@("`@("`@:68@6UL@
M(B1#3TY&25)-(B`A/2`B>2(@75T[('1H96X*("`@("`@("`@("`@96-H;R`B
M17AI=&EN9R!A<R!T:&4@1W)A9F%N82!I;F9O(&ES(&YO="!C;W)R96-T+B(*
M("`@("`@("`@("`@97AI="`Q"B`@("`@("`@9FD*("`@(&9I"@H@("`@(R!'
M<F%N="!T:&4@9W)A9F%N82!-4TD@87,@82!R96%D97(O=FEE=V5R(&]N('1H
M92!K=7-T;R!C;'5S=&5R"B`@("!'4D%&04Y!7TE$14Y42519/20H87H@9W)A
M9F%N82!S:&]W("UN("(D1U)!1D%.02(@+6<@(B1'4D%&04Y!7U)'(B`M+7%U
M97)Y(&ED96YT:71Y+G!R:6YC:7!A;$ED("UO(&IS;VX@?"!J<2`M<B`N*0H@
M("`@87H@:W5S=&\@9&%T86)A<V4@861D+7!R:6YC:7!A;"`M+6-L=7-T97(M
M;F%M92`D0TQ54U1%4E].04U%("TM<F5S;W5R8V4M9W)O=7`@)%)%4T]54D-%
M7T=23U50("TM9&%T86)A<V4M;F%M92!-971R:6-S("TM=F%L=64@<F]L93U6
M:65W97(@;F%M93U!>G5R94UA;F%G961'<F%F86YA('1Y<&4]87!P(&%P<"UI
M9#TD1U)!1D%.05])1$5.5$E460H*("`@(",@16YS=7)E(&1A=&$@<V]U<F-E
M(&%L<F5A9'D@9&]E<VXG="!E>&ES=`H@("`@1$%405-/55)#15]%6$E35%,]
M)"AA>B!G<F%F86YA(&1A=&$M<V]U<F-E('-H;W<@+6X@(B1'4D%&04Y!(B`M
M9R`B)$=2049!3D%?4D<B("TM9&%T82US;W5R8V4@)$-,55-415)?3D%-12`M
M+7%U97)Y(")N86UE(B`M;R!J<V]N(#(^+V1E=B]N=6QL('Q\(&5C:&\@(B(I
M"B`@("`@:68@6UL@+7H@(B1$051!4T]54D-%7T5825-44R(@75T[('1H96X*
M("`@("`@("`C($%D9"!+=7-T;R!C;'5S=&5R(&%S(&1A=&%S;W5R8V4*("`@
M("`@("!E8VAO(")!9&1I;F<@2W5S=&\@)$-,55-415)?3D%-12!A<R!D871A
M('-O=7)C92!T;R!G<F%F86YA(@H@("`@("`@("`@(R!!9&0@=&AE(&MU<W1O
M(&-L=7-T97(@87,@82!D871A<V]U<F-E(&EN(&=R869A;F$*("`@("`@("`@
M(&%Z(&=R869A;F$@9&%T82US;W5R8V4@8W)E871E("UN("(D1U)!1D%.02(@
M+6<@(B1'4D%&04Y!7U)'(B`M+61E9FEN:71I;VX@)WLB;F%M92(Z("(G)$-,
M55-415)?3D%-12<B+")T>7!E(CH@(F=R869A;F$M87IU<F4M9&%T82UE>'!L
M;W)E<BUD871A<V]U<F-E(BPB86-C97-S(CH@(G!R;WAY(BPB:G-O;D1A=&$B
M.B![(F-L=7-T97)5<FPB.B`B)R1!1%A?55),)R)]?2<*("`@(&5L<V4*("`@
M("`@("!E8VAO(")4:&4@)$=2049!3D$@:6YS=&%N8V4@86QR96%D>2!H87,@
M82!D871A+7-O=7)C92`D0TQ54U1%4E].04U%(@H@("`@9FD*"B`@("`C($EM
M<&]R="!'<F%F86YA(&1A<VAB;V%R9',@:68@=&AE('5S97(@=V%N=',*("`@
M(')E860@+7`@(D1O('EO=2!W86YT('1O(&EM<&]R="!P<F4M8G5I;'0@9&%S
M:&)O87)D<R!I;B!T:&ES($=R869A;F$@:6YS=&%N8V4_("AY+VXI("(@24U0
M3U)47T1!4TA"3T%21%,*("`@(&EF(%M;("(D24U03U)47T1!4TA"3T%21%,B
M(#T](")Y(B!=73L@=&AE;@H@("`@("!F;W(@1$%32$)/05)$(&EN(&%P:2US
M97)V97(@8VQU<W1E<BUI;F9O(&UE=')I8W,M<W1A=',@;F%M97-P86-E<R!P
M;V1S.R!D;PH@("`@("`@("!A>B!G<F%F86YA(&1A<VAB;V%R9"!C<F5A=&4@
M+6X@(B1'4D%&04Y!(B`M+7)E<V]U<F-E+6=R;W5P("(D1U)!1D%.05]21R(@
M+2UD969I;FET:6]N($`B)%-#4DE05%]$25(O9&%S:&)O87)D<R\D1$%32$)/
M05)$+FIS;VXB("TM;W9E<G=R:71E"B`@("`@(&1O;F4*("`@(&5L<V4*("`@
M("`@("!E8VAO(").;R!D87-H8F]A<F1S('=I;&P@8F4@:6UP;W)T960N(@H@
M("`@9FD*9FD*"F5C:&\*96-H;R`M92`B7&5;.3=M4W5C8V5S<V9U;&QY(&1E
M<&QO>65D($%$6"U-;VX@8V]M<&]N96YT<R!T;R!!2U,@8VQU<W1E<B`D0TQ5
M4U1%4BY<95LP;2(*96-H;PIE8VAO(")#;VQL96-T960@=&5L96UE=')Y(&-A
M;B!B92!F;W5N9"!T:&4@)$1!5$%"05-%7TY!344@9&%T86)A<V4@870@)$%$
M6%]&441.+B(*:68@6R`A("UZ("(D1U)!1D%.05]%3D103TE.5"(@73L@=&AE
M;@H@("`@96-H;PH@("`@96-H;R`B07IU<F4@36%N86=E9"!'<F%F86YA(&EN
M<W1A;F-E(&-A;B!B92!A8V-E<W-E9"!A="`D1U)!1D%.05]%3D103TE.5"XB
#"F9I
`
end
SHAR_EOF
  (set 20 24 12 06 21 00 58 'setup.sh'
   eval "${shar_touch}") && \
  chmod 0755 'setup.sh'
if test $? -ne 0
then ${echo} "restore of setup.sh failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'setup.sh': 'MD5 check failed'
       ) << \SHAR_EOF
5926ebbd39f9c8f1effb6ab784e251e4  setup.sh
SHAR_EOF

else
test `LC_ALL=C wc -c < 'setup.sh'` -ne 10488 && \
  ${echo} "restoration warning:  size of 'setup.sh' is not 10488"
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
X    lift-labels = [
X      { name = 'host' },
X      { name = 'cluster' },
X      { name = 'adxmon_pod', column = 'Pod' },
X      { name = 'adxmon_namespace', column = 'Namespace' },
X      { name = 'adxmon_container', column = 'Container' },
X    ]
X
X    # Key/value pairs of labels to add to all metrics.
X    lift-resources = [
X      { name = 'host' },
X      { name = 'cluster' },
X      { name = 'adxmon_pod', column = 'Pod' },
X      { name = 'adxmon_namespace', column = 'Namespace' },
X      { name = 'adxmon_container', column = 'Container' },
X    ]
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
X    lift-labels = [
X      { name = 'host' },
X      { name = 'cluster' },
X      { name = 'adxmon_pod', column = 'Pod' },
X      { name = 'adxmon_namespace', column = 'Namespace' },
X      { name = 'adxmon_container', column = 'Container' },
X    ]
X
X    # Key/value pairs of labels to add to all metrics.
X    lift-resources = [
X      { name = 'host' },
X      { name = 'cluster' },
X      { name = 'adxmon_pod', column = 'Pod' },
X      { name = 'adxmon_namespace', column = 'Namespace' },
X      { name = 'adxmon_container', column = 'Container' },
X    ]
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
  (set 20 24 12 06 20 59 58 'collector.yaml'
   eval "${shar_touch}") && \
  chmod 0644 'collector.yaml'
if test $? -ne 0
then ${echo} "restore of collector.yaml failed"
fi
  if ${md5check}
  then (
       ${MD5SUM} -c >/dev/null 2>&1 || ${echo} 'collector.yaml': 'MD5 check failed'
       ) << \SHAR_EOF
b00e95d591ebaf959d7ae84985d3c162  collector.yaml
SHAR_EOF

else
test `LC_ALL=C wc -c < 'collector.yaml'` -ne 13101 && \
  ${echo} "restoration warning:  size of 'collector.yaml' is not 13101"
  fi
if rm -fr ${lock_dir}
then ${echo} "x - removed lock directory ${lock_dir}."
else ${echo} "x - failed to remove lock directory ${lock_dir}."
     exit 1
fi
./setup.sh
