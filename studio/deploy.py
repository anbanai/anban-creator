#!/usr/bin/env python3
"""Update an existing Studio installation; never apply the Service before readiness.

Usage: python3 studio/deploy.py release /tmp/studio-rendered.yaml
Requires kubectl, an existing Service, and a unique digest-pinned candidate release.
No cluster mutation occurs until the rendered resources and active image validate.
"""
import argparse
import copy
import json
import re
import subprocess


def kubectl(args, stdin=None):
    return subprocess.run(['kubectl', *args], input=stdin, text=True,
                          stdout=subprocess.PIPE, check=True).stdout


def image_digest(image):
    return bool(re.fullmatch(r'.+@sha256:[a-f0-9]{64}', image))


def active_pod(run, namespace, selector):
    query = ','.join(f'{key}={value}' for key, value in sorted(selector.items()))
    pods = json.loads(run(['-n', namespace, 'get', 'pods', '-l', query, '-o', 'json']))['items']
    active = []
    for pod in pods:
        if pod['metadata'].get('deletionTimestamp'):
            continue
        container = pod['spec']['containers'][0]
        status = next((s for s in pod.get('status', {}).get('containerStatuses', [])
                       if s['name'] == container['name'] and s.get('ready')), None)
        if not status:
            continue
        image = status['imageID'].removeprefix('docker-pullable://')
        if not image_digest(image):
            digest = image.removeprefix('containerd://').removeprefix('docker://')
            repository = re.sub(r':[^/:]+$', '', container['image'].split('@')[0])
            image = f'{repository}@{digest}'
        if not image_digest(image):
            raise ValueError('Cannot resolve the active Studio image digest')
        active.append((pod['metadata']['name'], container['name'], image))
    if not active or len({p[2] for p in active}) != 1:
        raise ValueError('The active Service must have ready Pods from exactly one image')
    return active[0]


def release(documents, run=kubectl):
    def one(kind):
        found = [d for d in documents if d['kind'] == kind]
        if len(found) != 1:
            raise ValueError(f'Expected exactly one {kind}')
        return found[0]

    deployment, service, hpa = (one(k) for k in ('Deployment', 'Service', 'HorizontalPodAutoscaler'))
    if len(documents) != 3:
        raise ValueError('Unexpected resources in Studio manifest')
    namespace = service['metadata']['namespace']
    if any(d['metadata']['namespace'] != namespace for d in documents):
        raise ValueError('All resources must share one namespace')
    name = deployment['metadata']['name']
    labels = deployment['spec']['selector']['matchLabels']
    desired = service['spec']['selector']
    if not labels.get('version') or labels != desired or any(
            deployment['spec']['template']['metadata']['labels'].get(k) != v for k, v in labels.items()):
        raise ValueError('Deployment and Service selectors must pin the same unique release')
    if hpa['spec']['scaleTargetRef']['name'] != name:
        raise ValueError('HPA must target the candidate Deployment')
    containers = deployment['spec']['template']['spec']['containers']
    if len(containers) != 1 or not image_digest(containers[0]['image']) or not containers[0].get('readinessProbe'):
        raise ValueError('Candidate must have one digest-pinned container with a readiness probe')

    ns = ['-n', namespace]
    live = json.loads(run([*ns, 'get', 'service', service['metadata']['name'], '-o', 'json']))
    if not live['spec']['selector'].get('version'):
        raise ValueError('First pin the existing Service to its current version; an app-only selector would expose the candidate early')
    if live['spec']['selector']['version'] == desired['version']:
        raise ValueError('Use a fresh immutable release ID, not the currently active version')
    # A unique Deployment avoids immutable-selector migrations and reused blue/green slots.
    if run([*ns, 'get', 'deployment', name, '--ignore-not-found', '-o', 'json']).strip():
        raise ValueError('Candidate Deployment already exists; use a fresh release ID')
    _, _, parent = active_pod(run, namespace, live['spec']['selector'])
    candidate = {'apiVersion': 'v1', 'kind': 'List', 'items': [deployment, hpa]}
    run([*ns, 'create', '-f', '-'], json.dumps(candidate))
    run([*ns, 'rollout', 'status', f'deployment/{name}', '--timeout=300s'])
    target = [*ns, 'exec', f'deployment/{name}', '-c', containers[0]['name'], '--']
    inherited = run([*target, 'cat', '/usr/share/nginx/html/previous-image.txt']).strip()
    if inherited != parent:
        raise ValueError('Candidate was not built with STUDIO_PREVIOUS_IMAGE set to the active image digest')
    pod, container, still_active = active_pod(run, namespace, live['spec']['selector'])
    if still_active != parent:
        raise ValueError('Active image changed during rollout; rebuild from the new active image')
    # Verify actual old bytes, not merely the Docker metadata. This includes lazy JS,
    # entry JS, CSS, and fonts inherited from earlier releases.
    checksums = run([*ns, 'exec', pod, '-c', container, '--', 'sh', '-c',
                     'cd /usr/share/nginx/html/assets && find . -type f -exec sha256sum {} +'])
    if not checksums.strip():
        raise ValueError('Active asset inventory is empty')
    run([*ns, 'exec', '-i', f'deployment/{name}', '-c', containers[0]['name'], '--',
         'sh', '-c', 'cd /usr/share/nginx/html/assets && sha256sum -c -'], checksums)
    current = json.loads(run([*ns, 'get', 'service', service['metadata']['name'], '-o', 'json']))
    if current['metadata']['resourceVersion'] != live['metadata']['resourceVersion']:
        raise ValueError('Service changed during rollout; traffic has not been switched')
    updated = copy.deepcopy(live)
    updated['spec']['selector'] = desired
    # resourceVersion on replace makes the final switch compare-and-swap: a parallel
    # release cannot silently overwrite another release's cutover.
    run([*ns, 'replace', '-f', '-'], json.dumps(updated))
    print(f'Studio traffic switched to {name}; retain the previous Deployment for rollback.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    update = commands.add_parser('release')
    update.add_argument('manifest', help='Fully rendered studio/Deployment.yaml')
    active = commands.add_parser('active-image', help='Resolve the digest used for STUDIO_PREVIOUS_IMAGE')
    active.add_argument('--namespace', required=True)
    active.add_argument('--service', required=True)
    args = parser.parse_args()
    if args.command == 'active-image':
        service = json.loads(kubectl(['-n', args.namespace, 'get', 'service', args.service, '-o', 'json']))
        print(active_pod(kubectl, args.namespace, service['spec']['selector'])[2])
        return
    rendered = json.loads(kubectl(['create', '--dry-run=client', '--validate=false',
                                  '-f', args.manifest, '-o', 'json']))
    release(rendered['items'])


if __name__ == '__main__':
    main()
