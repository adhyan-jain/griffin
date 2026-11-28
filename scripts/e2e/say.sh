#!/bin/bash
# say.sh "<message>" [conversation-id-file-name]  -> sends one JJ chat message to the local server, keeps the thread id
E2E=${E2E_DIR:-/tmp/griffin-e2e}
THREAD=${2:-default}; CF=$E2E/conv-$THREAD
[ -f "$CF" ] || python3 -c "import uuid;print(uuid.uuid4())" > "$CF"
body=$(python3 -c "import json,sys;print(json.dumps({'message':sys.argv[1],'conversation_id':open(sys.argv[2]).read().strip()}))" "$1" "$CF")
curl -s -m 60 -X POST http://127.0.0.1:18080/api/v1/jj/messages -H "Authorization: Bearer $(cat $E2E/token)" -H 'Content-Type: application/json' -d "$body" |
python3 -c "
import json,sys
d=json.load(sys.stdin); d=d.get('data',d)
print('kind:', d.get('kind'), '| task:', d.get('task_id','-'))
print('JJ:', (d.get('response') or d.get('message') or '')[:700])
for a in d.get('actions') or []: print('  action:', a.get('tool'), '-', a.get('summary'), '(pending)' if a.get('pending') else '', '(undo '+a['undo_id']+')' if a.get('undo_id') else '')
"
