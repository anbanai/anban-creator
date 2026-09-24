"""Run with: python3 -m unittest discover -s studio -p '*_test.py'."""
import copy
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('studio_deploy', Path(__file__).with_name('deploy.py'))
deploy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(deploy)

OLD = 'registry/studio@sha256:' + '1' * 64
NEW = 'registry/studio@sha256:' + '2' * 64


def resources():
    return [
        {'apiVersion': 'apps/v1', 'kind': 'Deployment', 'metadata': {'name': 'studio-new', 'namespace': 'test'},
         'spec': {'selector': {'matchLabels': {'app': 'studio', 'version': 'new'}},
                  'template': {'metadata': {'labels': {'app': 'studio', 'version': 'new'}},
                               'spec': {'containers': [{'name': 'studio-new', 'image': NEW, 'readinessProbe': {'httpGet': {'path': '/index.html', 'port': 80}}}]}}}},
        {'apiVersion': 'v1', 'kind': 'Service', 'metadata': {'name': 'studio-svc', 'namespace': 'test'},
         'spec': {'selector': {'app': 'studio', 'version': 'new'}}},
        {'apiVersion': 'autoscaling/v2', 'kind': 'HorizontalPodAutoscaler',
         'metadata': {'name': 'studio-new', 'namespace': 'test'},
         'spec': {'scaleTargetRef': {'name': 'studio-new'}}},
    ]


class Cluster:
    def __init__(self):
        self.calls = []
        self.service = {'apiVersion': 'v1', 'kind': 'Service',
                        'metadata': {'name': 'studio-svc', 'namespace': 'test', 'resourceVersion': '42', 'annotations': {'keep': 'yes'}},
                        'spec': {'selector': {'app': 'studio', 'version': 'old'}, 'clusterIP': '10.0.0.1'}}
        self.parent = OLD
        self.fail_rollout = False
        self.fail_assets = False
        self.concurrent = False
        self.collision = False
        self.mixed = False
        self.pod = {'metadata': {'name': 'studio-old-pod'},
                    'spec': {'containers': [{'name': 'nginx', 'image': OLD}]},
                    'status': {'containerStatuses': [{'name': 'nginx', 'ready': True, 'imageID': OLD}]}}

    def __call__(self, args, stdin=None):
        self.calls.append((args, stdin))
        if 'get' in args and 'service' in args:
            return json.dumps(self.service)
        if 'get' in args and 'pods' in args:
            pods = [self.pod]
            if self.mixed:
                other = copy.deepcopy(self.pod)
                other['status']['containerStatuses'][0]['imageID'] = NEW
                pods.append(other)
            return json.dumps({'items': pods})
        if 'get' in args and 'deployment' in args:
            return ''
        if 'create' in args and self.collision:
            raise RuntimeError('AlreadyExists: another release claimed this ID')
        if 'rollout' in args:
            if self.fail_rollout:
                raise RuntimeError('readiness timed out')
            if self.concurrent:
                self.service['metadata']['resourceVersion'] = '43'
            return ''
        if 'exec' in args:
            if 'cat' in args:
                return self.parent + '\n'
            if '-i' in args:
                if self.fail_assets:
                    raise RuntimeError('old asset missing')
                return './old.js: OK\n'
            return 'a' * 64 + '  ./old.js\n'
        return ''


class DeploymentTests(unittest.TestCase):
    def test_candidate_ready_and_old_assets_verified_before_atomic_switch(self):
        cluster = Cluster()
        deploy.release(resources(), cluster)
        calls = cluster.calls
        apply = next(i for i, (args, _) in enumerate(calls) if 'create' in args)
        wait = next(i for i, (args, _) in enumerate(calls) if 'rollout' in args)
        switch = next(i for i, (args, _) in enumerate(calls) if 'replace' in args)
        self.assertLess(apply, wait)
        self.assertLess(wait, switch)
        self.assertNotIn('Service', [r['kind'] for r in json.loads(calls[apply][1])['items']])
        result = json.loads(calls[switch][1])
        self.assertEqual(result['metadata']['resourceVersion'], '42')
        self.assertEqual(result['metadata']['annotations'], {'keep': 'yes'})
        self.assertEqual(result['spec']['clusterIP'], '10.0.0.1')
        self.assertEqual(result['spec']['selector'], {'app': 'studio', 'version': 'new'})

    def test_never_switches_if_readiness_assets_or_parent_verification_fails(self):
        for fault in ('fail_rollout', 'fail_assets', 'parent', 'concurrent', 'collision', 'mixed'):
            with self.subTest(fault=fault):
                cluster = Cluster()
                setattr(cluster, fault, 'wrong-image' if fault == 'parent' else True)
                with self.assertRaises((ValueError, RuntimeError)):
                    deploy.release(resources(), cluster)
                self.assertFalse(any('replace' in args for args, _ in cluster.calls))

    def test_rejects_reused_versions_overlapping_selectors_and_mutable_images(self):
        for fault in ('selector', 'version', 'image', 'readiness'):
            with self.subTest(fault=fault):
                docs = copy.deepcopy(resources())
                if fault == 'selector':
                    del docs[0]['spec']['selector']['matchLabels']['version']
                elif fault == 'version':
                    docs[1]['spec']['selector']['version'] = 'old'
                elif fault == 'image':
                    docs[0]['spec']['template']['spec']['containers'][0]['image'] = 'studio:latest'
                else:
                    del docs[0]['spec']['template']['spec']['containers'][0]['readinessProbe']
                cluster = Cluster()
                with self.assertRaises(ValueError):
                    deploy.release(docs, cluster)
                self.assertFalse(any('create' in args for args, _ in cluster.calls))

    def test_rejects_legacy_app_only_service_before_creating_any_candidate(self):
        cluster = Cluster()
        del cluster.service['spec']['selector']['version']
        with self.assertRaises(ValueError):
            deploy.release(resources(), cluster)
        self.assertFalse(any('create' in args for args, _ in cluster.calls))


if __name__ == '__main__':
    unittest.main()
