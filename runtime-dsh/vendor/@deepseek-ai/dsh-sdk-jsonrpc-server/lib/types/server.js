/**
 * JSON-RPC methods and notifications for out-of-process harness SDKs.
 * The surrounding context owns plugins, persistence, and configured adapters.
 *
 * @module @deepseek-ai/dsh-sdk-jsonrpc-server/server
 */
import { resolve } from 'node:path';
import { brandString } from '@deepseek-ai/dsh-brand';
import { admitEncodedImages } from '@deepseek-ai/dsh-attachment';
import { createUserMessage, ReasoningEffortId } from '@deepseek-ai/dsh-llm';
import { carrierKeyOf } from '@deepseek-ai/dsh-scope';
import * as LlmDeepSeek from '@deepseek-ai/dsh-llm-deepseek-api-key';
function encodedImage(block) {
    return block.type === 'image' && 'data' in block;
}
async function durablePromptContent(ctx, blocks) {
    const images = blocks.filter(encodedImage);
    if (images.length === 0)
        return blocks;
    const attachments = ctx.get('attachments');
    if (attachments === undefined)
        throw new Error('SDK image prompt requires an attachment store');
    const refs = await admitEncodedImages(attachments, images.map((image) => ({
        data: image.data,
        mediaType: image.mimeType,
    })));
    let next = 0;
    return blocks.map(block => encodedImage(block)
        ? { type: 'image', attachment: refs[next++] }
        : block);
}
/** Recover the delegating parent from the service-owned scoped carrier. */
function subagentParentOf(carrier) {
    return carrierKeyOf(carrier);
}
function successStatus(reason, options) {
    if (reason === 'completed')
        return 'ok';
    return reason === 'max-tokens' && options.maxTokensAsSuccess === true ? 'ok' : 'error';
}
/**
 * SDK server over one booted harness context and transport peer. Construction
 * subscribes to session, agent, and subagent lifecycle events until shutdown;
 * reinitialization is unsupported.
 */
export class HarnessSdkJsonRpcServer {
    ctx;
    transport;
    options;
    cwd = process.cwd();
    provider = 'deepseek-official';
    model = 'deepseek-official';
    reasoningEffort;
    maxTokens;
    llmFiber;
    sessions = new Map();
    sessionCreations = new Map();
    pendingQuestions = new Map();
    disposers = [];
    shutdownTask;
    shuttingDown = false;
    initialized = false;
    constructor(ctx, transport, options = {}) {
        this.ctx = ctx;
        this.transport = transport;
        this.options = options;
        const serverOptions = this.options;
        const handleHeadlessQuestion = this.handleHeadlessQuestion.bind(this);
        this.disposers.push(ctx.on('session/event', (session, event) => {
            const payload = { sessionId: String(session.id), event };
            this.transport.notify('session.event', payload);
        }));
        this.disposers.push(ctx.on('agent/status', ({ agent, status }) => {
            this.transport.notify('session.status', { sessionId: String(agent.session.id), status });
        }));
        this.disposers.push(ctx.on('session/created', (session) => {
            const parentSession = session.header.parentSession;
            if (parentSession === undefined)
                return;
            const payload = {
                parentSessionId: String(parentSession),
                childSessionId: String(session.id),
            };
            this.transport.notify('subagent.started', payload);
        }));
        this.disposers.push(ctx.on('subagent/end', function (info) {
            const parent = subagentParentOf(this);
            // This protocol reports only in-process child sessions. The service
            // snapshots the provider name and local flag through child disposal;
            // matching ids or parent lineage alone never establishes locality.
            if (!info.local)
                return;
            const payload = {
                provider: info.provider,
                agentId: String(info.id),
                parentSessionId: String(parent.session.id),
                childSessionId: String(info.id),
                status: successStatus(info.stopReason, serverOptions),
                stopReason: info.stopReason,
                ...(info.lastAssistantMessage === undefined
                    ? {}
                    : { lastAssistantMessage: [...info.lastAssistantMessage] }),
            };
            transport.notify('subagent.finished', payload);
        }));
        if (this.options.interactiveQuestions === true) {
            this.disposers.push(ctx.on('user-questions/request', function (request, next) {
                return handleHeadlessQuestion(carrierKeyOf(this), request, next);
            }));
        }
    }
    /**
     * Validate and configure the SDK route, mounting the DeepSeek fallback only when unowned.
     * @param params - SDK handshake parameters.
     * @returns server identity for the handshake.
     */
    async initialize(params) {
        if (params.reasoningEffort !== undefined
            && (typeof params.reasoningEffort !== 'string' || params.reasoningEffort.length === 0)) {
            throw new TypeError('initialize reasoningEffort must be a non-empty string');
        }
        if (params.maxTokens !== undefined
            && (!Number.isSafeInteger(params.maxTokens) || params.maxTokens <= 0)) {
            throw new TypeError('initialize maxTokens must be a positive safe integer');
        }
        const cwd = resolve(params.cwd);
        const provider = params.provider;
        const model = params.model;
        const reasoningEffort = params.reasoningEffort === undefined
            ? undefined
            : ReasoningEffortId(params.reasoningEffort);
        if (!this.hasAdapterFor(provider)) {
            if (provider !== 'deepseek-official')
                throw new Error(`no adapter registered for provider "${provider}"`);
            this.llmFiber = await this.ctx.plugin(LlmDeepSeek);
        }
        // Adapter presence was read from this service above; a successful fallback mount also requires it.
        const llm = this.ctx.get('llm');
        await llm.resolveCallConfig({
            provider,
            model,
            ...reasoningEffort === undefined ? {} : { reasoningEffort },
            ...params.maxTokens === undefined ? {} : { maxTokens: params.maxTokens },
        });
        this.cwd = cwd;
        this.provider = provider;
        this.model = model;
        this.reasoningEffort = reasoningEffort;
        this.maxTokens = params.maxTokens;
        this.initialized = true;
        return {
            serverInfo: { name: 'deepseek-harness-sdk-runtime', version: '0.0.1' },
            capabilities: { sessionStart: true, sessionAnswer: true },
        };
    }
    /**
     * Queue one identified prompt without assigning later activity to it.
     * @param params - target session and user content.
     * @returns the durable message identity.
     */
    async prompt(params) {
        if (!this.initialized)
            throw new Error('SDK server is not initialized');
        const rec = await this.getOrCreateSession(params.sessionId);
        // An agent-loop-only reload disposes the loop's agents while this record
        // survives; a retained agent accepts followup() silently, so validate the
        // record against the live registry before delivery.
        this.assertLiveAgent(rec, params.sessionId);
        const content = await durablePromptContent(this.ctx, params.contentBlocks);
        // Attachment admission crosses an async boundary where shutdown or an
        // agent-loop reload may detach the retained handle.
        this.assertLiveAgent(rec, params.sessionId);
        const message = createUserMessage({
            content,
            source: { kind: 'user' },
        });
        rec.handle.agent.followup(message);
        return { messageId: message.id };
    }
    /**
     * Create an empty session and bind one pre-registered DSH preset before any
     * model turn can start. Preset documents and plugin paths never cross this
     * protocol boundary.
     */
    async start(params) {
        if (!this.initialized)
            throw new Error('SDK server is not initialized');
        if (!params.sessionId.trim() || !params.presetId.trim()) {
            throw new TypeError('session/start requires non-empty sessionId and presetId');
        }
        const rec = await this.getOrCreateSession(params.sessionId, params.presetId);
        if (rec.presetId !== params.presetId) {
            throw new Error(`session ${params.sessionId} is already bound to a different preset`);
        }
        this.assertLiveAgent(rec, params.sessionId);
        if (!rec.started) {
            const start = rec.handle.agent.start;
            if (typeof start !== 'function') {
                throw new Error('session/start requires an Agent runtime with message-free start support');
            }
            try {
                start.call(rec.handle.agent);
                rec.started = true;
            }
            catch (error) {
                throw new Error(`session/start failed for ${params.sessionId}`, { cause: error });
            }
            this.transport.notify('session.ready', { sessionId: params.sessionId, presetId: params.presetId });
        }
        const result = { sessionId: params.sessionId, presetId: params.presetId };
        return result;
    }
    /**
     * Answer exactly one live root Agent question through DSH's existing
     * userQuestions service, preserving its call ownership and answer checks.
     */
    async answer(params) {
        if (!this.initialized)
            throw new Error('SDK server is not initialized');
        const rec = this.sessions.get(params.sessionId);
        if (rec === undefined)
            throw new Error(`unknown SDK session: ${params.sessionId}`);
        this.assertLiveAgent(rec, params.sessionId);
        const key = this.questionKey(params.sessionId, params.callId);
        const pending = this.pendingQuestions.get(key);
        if (pending !== undefined) {
            this.validateAnswer(pending.questions, params.answer);
            this.pendingQuestions.delete(key);
            pending.removeAbortListener();
            pending.resolve(this.cloneAnswer(params.answer));
            return { accepted: true };
        }
        const accepted = this.ctx.userQuestions.answer(rec.handle.agent, brandString(params.callId), params.answer);
        if (!accepted)
            throw new Error(`question is no longer pending: ${params.callId}`);
        return { accepted: true };
    }
    questionKey(sessionId, callId) {
        return `${sessionId}\u0000${callId}`;
    }
    cloneAnswer(answer) {
        return {
            answers: answer.answers.map(item => ({
                id: item.id,
                selected: [...item.selected],
                ...(item.custom === undefined ? {} : { custom: item.custom }),
            })),
        };
    }
    validateAnswer(questions, answer) {
        if (!Array.isArray(answer?.answers) || answer.answers.length !== questions.length) {
            throw new Error('session/answer must include exactly one answer for every question');
        }
        const questionById = new Map(questions.map(question => [question.id, question]));
        const seen = new Set();
        for (const item of answer.answers) {
            if (typeof item.id !== 'string' || seen.has(item.id)) {
                throw new Error('session/answer contains a duplicate or invalid question id');
            }
            const question = questionById.get(item.id);
            if (question === undefined)
                throw new Error(`session/answer references unknown question: ${item.id}`);
            if (!Array.isArray(item.selected) || item.selected.some(value => typeof value !== 'string')) {
                throw new Error(`session/answer has invalid selections for question: ${item.id}`);
            }
            if (question.multiSelect !== true && item.selected.length > 1) {
                throw new Error(`session/answer selects multiple options for single-select question: ${item.id}`);
            }
            const options = new Set((question.options ?? []).map(option => option.label));
            if (item.selected.some(value => !options.has(value))) {
                throw new Error(`session/answer selects an unavailable option for question: ${item.id}`);
            }
            if (item.custom !== undefined && typeof item.custom !== 'string') {
                throw new Error(`session/answer has invalid custom text for question: ${item.id}`);
            }
            seen.add(item.id);
        }
    }
    handleHeadlessQuestion(agent, request, next) {
        if (request.agent !== agent || request.wait?.callId === undefined)
            return next();
        const sessionId = String(agent.session.id);
        const rec = this.sessions.get(sessionId);
        if (rec === undefined || rec.handle.agent !== agent)
            return next();
        const callId = String(request.wait.callId);
        const key = this.questionKey(sessionId, callId);
        if (this.pendingQuestions.has(key)) {
            return Promise.reject(new Error(`question is already pending: ${callId}`));
        }
        if (request.signal?.aborted)
            return Promise.reject(new Error(`question was aborted: ${callId}`));
        let removeAbortListener = () => { };
        const promise = new Promise((resolve, reject) => {
            const pending = {
                agent,
                questions: request.questions,
                resolve,
                reject,
                removeAbortListener: () => removeAbortListener(),
            };
            this.pendingQuestions.set(key, pending);
            if (request.signal !== undefined) {
                const onAbort = () => {
                    if (this.pendingQuestions.get(key) !== pending)
                        return;
                    this.pendingQuestions.delete(key);
                    reject(new Error(`question was aborted: ${callId}`));
                };
                request.signal.addEventListener('abort', onAbort, { once: true });
                removeAbortListener = () => request.signal?.removeEventListener('abort', onAbort);
                pending.removeAbortListener = removeAbortListener;
            }
        });
        return promise;
    }
    assertLiveAgent(rec, sessionId) {
        if (this.ctx.agents.get(rec.handle.agent.id) !== rec.handle.agent) {
            throw new Error(`session agent was disposed outside the server: ${sessionId}`);
        }
    }
    /**
     * Dispose server-owned agents, adapter, and subscriptions to quiescence.
     * The surrounding context remains running.
     * @returns empty JSON-RPC result.
     */
    shutdown() {
        this.shutdownTask ??= this.performShutdown();
        return this.shutdownTask;
    }
    async performShutdown() {
        this.shuttingDown = true;
        for (const [key, pending] of this.pendingQuestions) {
            this.pendingQuestions.delete(key);
            pending.removeAbortListener();
            pending.reject(new Error('SDK server is shutting down'));
        }
        const pendingCreations = [...this.sessionCreations.values()];
        await Promise.allSettled(pendingCreations);
        this.sessionCreations.clear();
        const records = [...this.sessions.values()];
        this.sessions.clear();
        const failures = [];
        while (this.disposers.length > 0) {
            try {
                this.disposers.pop()?.();
            }
            catch (error) {
                failures.push(error);
            }
        }
        const teardownResults = await Promise.allSettled([
            ...records.map(rec => Promise.resolve().then(() => rec.handle.dispose())),
            ...(this.llmFiber === undefined ? [] : [Promise.resolve().then(() => this.llmFiber?.dispose())]),
        ]);
        this.llmFiber = undefined;
        failures.push(...teardownResults
            .filter((result) => result.status === 'rejected')
            .map(result => result.reason));
        if (failures.length === 1)
            throw failures[0];
        if (failures.length > 1)
            throw new AggregateError(failures, 'SDK server teardown failed');
        return {};
    }
    /**
     * Dispatch one incoming JSON-RPC request to its typed handler. Throws (→ a
     * JSON-RPC error response) on an unknown method.
     * @param method - the JSON-RPC method name.
     * @param params - the raw params object from the wire.
     * @returns the handler's result, to be serialized as the response.
     */
    async handleRequest(method, params) {
        switch (method) {
            case 'initialize':
                return this.initialize(params);
            case 'session/start':
                return this.start(params);
            case 'session/prompt':
                return this.prompt(params);
            case 'session/answer':
                return this.answer(params);
            case 'shutdown':
                return this.shutdown();
            default:
                throw new Error(`unknown DeepSeek Harness SDK runtime method: ${method}`);
        }
    }
    async getOrCreateSession(sessionId, presetId) {
        if (this.shuttingDown)
            throw new Error('SDK server is shutting down');
        const existing = this.sessions.get(sessionId);
        if (existing)
            return existing;
        const pending = this.sessionCreations.get(sessionId);
        if (pending)
            return pending;
        const creation = this.createSession(sessionId, presetId);
        this.sessionCreations.set(sessionId, creation);
        void creation.then(() => { this.sessionCreations.delete(sessionId); }, () => { this.sessionCreations.delete(sessionId); });
        return creation;
    }
    async createSession(sessionId, presetId) {
        // No preset composition: this server's compositions keep the model-facing
        // rows in the host plane, so this agent reads them from the global layer. A
        // deployment that configures a roster has to join one here first
        // (@deepseek-ai/dsh-agent-preset-registry README, "Composing a child agent").
        const handle = await this.ctx.agents.create({
            sessionId: brandString(sessionId),
            meta: { cwd: this.cwd },
            agentOptions: {
                provider: this.provider,
                model: this.model,
                ...this.reasoningEffort === undefined ? {} : { reasoningEffort: this.reasoningEffort },
                ...this.maxTokens === undefined ? {} : { maxTokens: this.maxTokens },
            },
        });
        if (presetId !== undefined) {
            const presets = this.ctx.get('agentPresets');
            if (presets === undefined) {
                await handle.dispose();
                throw new Error('session/start requires the DSH agent preset registry');
            }
            try {
                await presets.select(handle.agent, presetId);
            }
            catch (error) {
                await handle.dispose();
                throw error;
            }
        }
        const rec = { handle, started: false, ...(presetId === undefined ? {} : { presetId }) };
        this.sessions.set(sessionId, rec);
        return rec;
    }
    hasAdapterFor(provider) {
        return this.ctx.get('llm')?.listProviders().some(entry => entry.id === provider) ?? false;
    }
}
//# sourceMappingURL=server.js.map