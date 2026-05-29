export default {
    name: 'ChatwootConfig',
    props: ['connected'],
    data() {
        return {
            configs: [],
            loading: false,
            saving: false,
            editing: false,
            form: this.emptyForm(),
        }
    },
    computed: {
        deviceOptions() {
            if (!this.connected || this.connected.length === 0) return [];
            return this.connected
                .map(d => d.jid || d.device || d.id)
                .filter(Boolean);
        }
    },
    methods: {
        emptyForm() {
            return {
                device_id: '',
                chatwoot_url: '',
                api_token: '',
                account_id: null,
                inbox_mode: 'create', // 'create' = new inbox by name, 'existing' = use inbox_id
                inbox_id: null,
                inbox_name: '',
                enabled: true,
                import_messages: false,
                days_limit: 3,
            };
        },
        async openModal() {
            try {
                await this.fetchConfigs();
                this.resetForm();
                $('#modalChatwootConfig').modal({ observeChanges: true }).modal('show');
            } catch (err) {
                showErrorInfo(this.errMsg(err));
            }
        },
        resetForm() {
            this.editing = false;
            this.form = this.emptyForm();
            // Happy path: pre-select the only connected device.
            if (this.deviceOptions.length === 1) {
                this.form.device_id = this.deviceOptions[0];
            }
        },
        async fetchConfigs() {
            this.loading = true;
            try {
                const response = await window.http.get(`/chatwoot/configs`);
                this.configs = response.data.results || [];
            } finally {
                this.loading = false;
            }
        },
        editConfig(cfg) {
            this.editing = true;
            // Clone so edits don't mutate the table row until saved. Editing always
            // targets the existing inbox_id.
            this.form = Object.assign(this.emptyForm(), cfg, { inbox_mode: 'existing', inbox_name: '' });
        },
        async saveConfig() {
            if (!this.form.chatwoot_url || !this.form.api_token) {
                showErrorInfo('chatwoot_url and api_token are required');
                return;
            }
            if (!this.form.account_id) {
                showErrorInfo('account_id is required');
                return;
            }
            const creatingInbox = this.form.inbox_mode === 'create';
            if (creatingInbox && !this.form.inbox_name) {
                showErrorInfo('inbox_name is required to create a new inbox');
                return;
            }
            if (!creatingInbox && !this.form.inbox_id) {
                showErrorInfo('inbox_id is required');
                return;
            }
            this.saving = true;
            try {
                const payload = {
                    chatwoot_url: this.form.chatwoot_url,
                    api_token: this.form.api_token,
                    account_id: Number(this.form.account_id),
                    enabled: !!this.form.enabled,
                    import_messages: !!this.form.import_messages,
                    days_limit: Number(this.form.days_limit) || 3,
                };
                // device_id is optional on create; the backend auto-selects the
                // single connected device when omitted.
                if (this.form.device_id) {
                    payload.device_id = this.form.device_id;
                }
                if (creatingInbox) {
                    payload.inbox_name = this.form.inbox_name;
                } else {
                    payload.inbox_id = Number(this.form.inbox_id);
                }
                if (this.editing) {
                    await window.http.put(`/chatwoot/configs/${encodeURIComponent(this.form.device_id)}`, payload);
                    showSuccessInfo('Mapping updated');
                } else {
                    await window.http.post(`/chatwoot/configs`, payload);
                    showSuccessInfo('Mapping created');
                }
                await this.fetchConfigs();
                this.resetForm();
            } catch (err) {
                showErrorInfo(this.errMsg(err));
            } finally {
                this.saving = false;
            }
        },
        async toggleEnabled(cfg) {
            try {
                const payload = Object.assign({}, cfg, { enabled: !cfg.enabled });
                await window.http.put(`/chatwoot/configs/${encodeURIComponent(cfg.device_id)}`, payload);
                await this.fetchConfigs();
                showSuccessInfo(payload.enabled ? 'Mapping enabled' : 'Mapping disabled');
            } catch (err) {
                showErrorInfo(this.errMsg(err));
            }
        },
        async deleteConfig(cfg) {
            if (!confirm(`Delete Chatwoot mapping for ${cfg.device_id}?`)) return;
            try {
                await window.http.delete(`/chatwoot/configs/${encodeURIComponent(cfg.device_id)}`);
                await this.fetchConfigs();
                if (this.editing && this.form.device_id === cfg.device_id) {
                    this.resetForm();
                }
                showSuccessInfo('Mapping deleted');
            } catch (err) {
                showErrorInfo(this.errMsg(err));
            }
        },
        errMsg(err) {
            if (err && err.response && err.response.data && err.response.data.message) {
                return err.response.data.message;
            }
            return err && err.message ? err.message : String(err);
        },
    },
    template: `
    <div class="teal card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui teal right ribbon label">Chatwoot</a>
            <div class="header">Chatwoot Inboxes</div>
            <div class="description">
                Map each device to its own Chatwoot inbox
            </div>
        </div>
    </div>

    <div class="ui large modal" id="modalChatwootConfig">
        <i class="close icon"></i>
        <div class="header">Chatwoot Device &#8594; Inbox Mappings</div>
        <div class="content">
            <div v-if="loading" class="ui active centered inline loader"></div>

            <table v-else class="ui celled compact table">
                <thead>
                    <tr>
                        <th>Device (JID)</th>
                        <th>Chatwoot URL</th>
                        <th>Account</th>
                        <th>Inbox</th>
                        <th>Enabled</th>
                        <th>Action</th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-if="configs.length === 0">
                        <td colspan="6" style="text-align:center">No mappings yet</td>
                    </tr>
                    <tr v-for="cfg in configs" :key="cfg.device_id">
                        <td>{{ cfg.device_id }}</td>
                        <td>{{ cfg.chatwoot_url }}</td>
                        <td>{{ cfg.account_id }}</td>
                        <td>{{ cfg.inbox_id }}</td>
                        <td>
                            <span :class="['ui tiny label', cfg.enabled ? 'green' : 'grey']">
                                {{ cfg.enabled ? 'on' : 'off' }}
                            </span>
                        </td>
                        <td>
                            <div style="display:flex; gap:6px;">
                                <button class="ui blue tiny button" @click="editConfig(cfg)">Edit</button>
                                <button class="ui tiny button" @click="toggleEnabled(cfg)">
                                    {{ cfg.enabled ? 'Disable' : 'Enable' }}
                                </button>
                                <button class="ui red tiny button" @click="deleteConfig(cfg)">Delete</button>
                            </div>
                        </td>
                    </tr>
                </tbody>
            </table>

            <div class="ui horizontal divider">{{ editing ? 'Edit mapping' : 'Add mapping' }}</div>

            <form class="ui form" @submit.prevent="saveConfig">
                <div class="two fields">
                    <div class="field">
                        <label>Device (WhatsApp JID)</label>
                        <select v-model="form.device_id" :disabled="editing">
                            <option value="">(auto-detect single device)</option>
                            <option v-for="jid in deviceOptions" :key="jid" :value="jid">{{ jid }}</option>
                        </select>
                        <div class="ui small message" v-if="!editing && deviceOptions.length === 0" style="margin-top:6px">
                            No connected device listed; the server will auto-select if exactly one is logged in.
                        </div>
                    </div>
                    <div class="field">
                        <label>Chatwoot URL</label>
                        <input type="text" v-model="form.chatwoot_url" placeholder="https://chatwoot.example.com">
                    </div>
                </div>
                <div class="field">
                    <label>API Token</label>
                    <input type="text" v-model="form.api_token" placeholder="api_access_token">
                </div>
                <div class="two fields">
                    <div class="field">
                        <label>Account ID</label>
                        <input type="number" v-model="form.account_id" placeholder="2">
                    </div>
                    <div class="field">
                        <label>Days Limit (import)</label>
                        <input type="number" v-model="form.days_limit" placeholder="3">
                    </div>
                </div>
                <div class="grouped fields">
                    <label>Inbox</label>
                    <div class="field">
                        <div class="ui radio checkbox">
                            <input type="radio" value="create" v-model="form.inbox_mode" :disabled="editing" id="cwInboxCreate">
                            <label for="cwInboxCreate">Create new inbox (auto)</label>
                        </div>
                    </div>
                    <div class="field">
                        <div class="ui radio checkbox">
                            <input type="radio" value="existing" v-model="form.inbox_mode" id="cwInboxExisting">
                            <label for="cwInboxExisting">Use existing inbox ID</label>
                        </div>
                    </div>
                </div>
                <div class="field" v-if="form.inbox_mode === 'create'">
                    <label>New inbox name</label>
                    <input type="text" v-model="form.inbox_name" placeholder="WhatsApp 573166203787">
                </div>
                <div class="field" v-else>
                    <label>Inbox ID</label>
                    <input type="number" v-model="form.inbox_id" placeholder="67">
                </div>
                <div class="two fields">
                    <div class="field">
                        <div class="ui checkbox">
                            <input type="checkbox" v-model="form.enabled" id="cwEnabled">
                            <label for="cwEnabled">Enabled</label>
                        </div>
                    </div>
                    <div class="field">
                        <div class="ui checkbox">
                            <input type="checkbox" v-model="form.import_messages" id="cwImport">
                            <label for="cwImport">Import messages</label>
                        </div>
                    </div>
                </div>
                <button class="ui teal button" type="submit" :class="{loading: saving}">
                    {{ editing ? 'Update' : 'Create' }}
                </button>
                <button v-if="editing" class="ui button" type="button" @click="resetForm">Cancel</button>
            </form>
        </div>
    </div>
    `
}
