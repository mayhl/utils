#!/usr/bin/env bash


if [ ! -v "${MY_CONFIG_IS_LOCAL}" ]; then
  # TODO: 
  #   [] Check ssh/ossh switching with rsync
  #   [] Add optional ossh/krb5 PATHS with defaults
  export PATH=/usr/local/krb5/bin:/usr/local/ossh/bin:$PATH
  export KRB5_CONFIG=/etc/krb5.conf
  export OSSH="/usr/local/ossh/bin/ssh"
  alias ossh='/usr/local/ossh/bin/ssh'
  alias hpc='pkinit ${HPC_UNAME} "$@"'
else

  OSSH=$(which ssh)
  alias ossh=ssh
fi

# Wrapper commands
cpHPCWrapper() {

  #echo "rsync ${HPC_RSYNC_OPTS} -e $OSSH $1 $2"
  rsync "${HPC_RSYNC_OPTS}" -e "$OSSH" $1 $2
}

cp2HPC() {
  cpHPCWrapper $2 $1:$3
}

cpHPC() {
  cpHPCWrapper $1:$2 $3
}

HPCS=()




for HPC_HOST in ${HPC_HOSTS}; do

  # Constructing variables names
  HOST=$(echo ${HPC_HOST} | tr '[a-z]' '[A-Z]')
  HOST_HPCS=${HOST}_HPCS
  HPC_HOST=${HOST}_HOST


  # Reading variable based on dynamic name 
  HOST_HPCS=(${(P)${:-${HOST_HPCS}}})
  HPC_HOST=(${(P)${:-${HPC_HOST}}})

  for HOST_HPC in ${HOST_HPCS}; do

    HPCS+=($HOST_HPC) 
    HOST_HPCU=$(echo ${HOST_HPC} | tr '[a-z]' '[A-Z]')
    HOST_HPCL=$(echo ${HOST_HPC} | tr '[a-zA-Z]' '[a-z]')

    typeset ${HOST_HPCU}_HOST=${HOST_HPCL}.${HPC_HOST}
  done

done

for HPC in ${HPCS}; do
  HPCU=$(echo ${HPC} | tr '[a-z]' '[A-Z]')
  HPCL=$(echo ${HPC} | tr '[A-Z]' '[a-z]')

  HPC_HOST=(${(P)${:-${HPCU}_HOST}})

  typeset ${HPCU}_SSH=${HPC_UNAME}@${HPC_HOST}

  alias ${HPCL}='ossh ${HPC_SSH_OPTS} "$@"'\${${HPCU}_SSH}
  HPCC=$(echo ${HPCL:0:1} | tr '[a-z]' '[A-Z]')${HPCL:1}
  alias cp2${HPCC}='cp2HPC '\${${HPCU}_SSH}' "$@"'
  alias cp${HPCC}='cpHPC '\${${HPCU}_SSH}' "$@"'
done
