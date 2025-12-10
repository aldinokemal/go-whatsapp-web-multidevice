import FormRecipient from "./generic/FormRecipient.js";

const STATUS_LABELS = {
    pending: { label: 'Pending', color: 'yellow' },
    sending: { label: 'Sending', color: 'blue' },
    sent: { label: 'Sent', color: 'green' },
    failed: { label: 'Failed', color: 'red' },
};

const SCHEDULE_MIN_LEAD_MS = 5000;

const DEFAULT_CONSTANTS = {
    TYPEUSER: "@s.whatsapp.net",
    TYPEGROUP: "@g.us",
    TYPENEWSLETTER: "@newsletter",
    TYPESTATUS: "status@broadcast",
};

const resolveConstant = (key) => {
    if (typeof window !== 'undefined' && window[key]) {
        return window[key];
    }
    return DEFAULT_CONSTANTS[key];
};

export default {
    name: 'ScheduleManager',
    components: {
        FormRecipient,
    },
    data() {
        const constants = {
            TYPEUSER: resolveConstant('TYPEUSER'),
            TYPEGROUP: resolveConstant('TYPEGROUP'),
            TYPENEWSLETTER: resolveConstant('TYPENEWSLETTER'),
            TYPESTATUS: resolveConstant('TYPESTATUS'),
        };

        return {
            constants,
            items: [],
            loading: false,
            actionLoading: null,
            deleteLoading: null,
            filter_statuses: ['pending', 'sending', 'failed'],
            statusOptions: [
                { value: 'pending', label: 'Pending' },
                { value: 'sending', label: 'Sending' },
                { value: 'sent', label: 'Sent' },
                { value: 'failed', label: 'Failed' },
            ],
            formMode: 'create',
            formVisible: false,
            formSubmitting: false,
            form: null,
        };
    },
    created() {
        this.resetForm();
    },
    methods: {
        blankForm() {
            return {
                id: null,
                type: this.constants.TYPEUSER,
                phone: '',
                message: '',
                reply_message_id: '',
                is_forwarded: false,
                duration: '',
                schedule_at: this.formatDatetimeLocal(this.defaultScheduleTime()),
            };
        },
        defaultScheduleTime() {
            return new Date(Date.now() + 15 * 60 * 1000);
        },
        formatDatetimeLocal(date) {
            if (!date) {
                return '';
            }
            const d = (date instanceof Date) ? new Date(date.getTime()) : new Date(date);
            if (Number.isNaN(d.getTime())) {
                return '';
            }
            const pad = (value) => `${value}`.padStart(2, '0');
            const year = d.getFullYear();
            const month = pad(d.getMonth() + 1);
            const day = pad(d.getDate());
            const hours = pad(d.getHours());
            const minutes = pad(d.getMinutes());
            return `${year}-${month}-${day}T${hours}:${minutes}`;
        },
        openModal() {
            this.fetchMessages();
            $('#modalScheduleManager').modal({
                onHidden: () => {
                    this.actionLoading = null;
                    this.deleteLoading = null;
                }
            }).modal('show');
            this.initializeCheckboxes();
        },
        initializeCheckboxes() {
            this.$nextTick(() => {
                $('#modalScheduleManager .ui.checkbox, #modalScheduleForm .ui.checkbox').checkbox();
            });
        },
        async fetchMessages() {
            if (this.loading) {
                return;
            }
            this.loading = true;
            try {
                const params = new URLSearchParams();
                if (this.filter_statuses.length > 0 && this.filter_statuses.length < this.statusOptions.length) {
                    params.append('statuses', this.filter_statuses.join(','));
                }
                const query = params.toString() ? `?${params.toString()}` : '';
                const response = await window.http.get(`/schedule/messages${query}`);
                this.items = response.data?.results?.items || [];
                this.items.sort((a, b) => new Date(a.schedule_at) - new Date(b.schedule_at));
                this.initializeCheckboxes();
            } catch (error) {
                this.handleError(error);
            } finally {
                this.loading = false;
            }
        },
        formatRecipient(phone) {
            if (!phone) {
                return '—';
            }
            if (phone === this.constants.TYPESTATUS) {
                return 'Status Broadcast';
            }
            const suffixes = [
                this.constants.TYPEGROUP,
                this.constants.TYPEUSER,
                this.constants.TYPENEWSLETTER,
            ];
            for (const suffix of suffixes) {
                if (phone.endsWith(suffix)) {
                    return phone.slice(0, -suffix.length);
                }
            }
            return phone;
        },
        recipientType(phone) {
            if (phone === this.constants.TYPESTATUS) {
                return this.constants.TYPESTATUS;
            }
            if (phone.endsWith(this.constants.TYPEGROUP)) {
                return this.constants.TYPEGROUP;
            }
            if (phone.endsWith(this.constants.TYPENEWSLETTER)) {
                return this.constants.TYPENEWSLETTER;
            }
            return this.constants.TYPEUSER;
        },
        formatStatus(status) {
            const meta = STATUS_LABELS[status];
            if (meta) {
                return `<span class="ui ${meta.color} label">${meta.label}</span>`;
            }
            return `<span class="ui grey label">Unknown</span>`;
        },
        formatTimestamp(value) {
            if (!value) {
                return '—';
            }
            return moment(value).local().format('LLL');
        },
        shorten(text) {
            if (!text) {
                return '—';
            }
            const trimmed = text.trim();
            if (trimmed.length <= 80) {
                return trimmed;
            }
            return `${trimmed.slice(0, 80)}…`;
        },
        handleError(error) {
            if (error && error.response && error.response.data && error.response.data.message) {
                showErrorInfo(error.response.data.message);
            } else if (error instanceof Error) {
                showErrorInfo(error.message);
            } else {
                showErrorInfo('Unexpected error');
            }
        },
        resetForm() {
            this.form = this.blankForm();
            this.formMode = 'create';
            this.formSubmitting = false;
        },
        openCreateForm() {
            this.resetForm();
            this.formVisible = true;
            $('#modalScheduleForm').modal({
                onHidden: () => {
                    this.resetForm();
                }
            }).modal('show');
            this.initializeCheckboxes();
        },
        openEditForm(item) {
            const type = this.recipientType(item.phone);
            const trimmedPhone = type === this.constants.TYPESTATUS ? '' : item.phone.replace(type, '');
            this.form = {
                id: item.id,
                type: type,
                phone: trimmedPhone,
                message: item.message || '',
                reply_message_id: item.reply_message_id || '',
                is_forwarded: Boolean(item.is_forwarded),
                duration: item.duration != null ? item.duration : '',
                schedule_at: this.formatDatetimeLocal(moment(item.schedule_at).local().toDate()),
            };
            this.formMode = 'edit';
            this.formVisible = true;
            $('#modalScheduleForm').modal({
                onHidden: () => {
                    this.resetForm();
                }
            }).modal('show');
            this.initializeCheckboxes();
        },
        buildPayload() {
            const payload = {
                phone: this.composePhone(),
                message: this.form.message.trim(),
                is_forwarded: this.form.is_forwarded,
                schedule_at: new Date(this.form.schedule_at).toISOString(),
            };
            if (this.form.reply_message_id.trim() !== '') {
                payload.reply_message_id = this.form.reply_message_id.trim();
            }
            if (this.form.duration !== '' && this.form.duration != null) {
                const parsedDuration = Number(this.form.duration);
                if (!Number.isNaN(parsedDuration) && parsedDuration >= 0) {
                    payload.duration = parsedDuration;
                }
            }
            return payload;
        },
        composePhone() {
            if (this.form.type === this.constants.TYPESTATUS) {
                return this.constants.TYPESTATUS;
            }
            return `${this.form.phone}${this.form.type}`;
        },
        isValidForm() {
            if (this.formMode === 'create' && !this.form.schedule_at) {
                return false;
            }
            const phoneValid = this.form.type === this.constants.TYPESTATUS || this.form.phone.trim().length > 0;
            const messageValid = this.form.message.trim().length > 0 && this.form.message.length <= 4096;
            if (!phoneValid || !messageValid) {
                return false;
            }

            if (!this.form.schedule_at) {
                return false;
            }

            const scheduleDate = new Date(this.form.schedule_at);
            if (Number.isNaN(scheduleDate.getTime())) {
                return false;
            }
            return scheduleDate.getTime() - Date.now() >= SCHEDULE_MIN_LEAD_MS;
        },
        async submitForm() {
            if (!this.isValidForm() || this.formSubmitting) {
                return;
            }
            this.formSubmitting = true;
            try {
                const payload = this.buildPayload();
                let response;
                if (this.formMode === 'create') {
                    response = await window.http.post(`/schedule/messages`, payload);
                } else {
                    response = await window.http.put(`/schedule/messages/${this.form.id}`, payload);
                }
                const message = response.data?.results;
                showSuccessInfo(this.formMode === 'create' ? 'Scheduled message created' : 'Scheduled message updated');
                this.updateLocalItems(message);
                $('#modalScheduleForm').modal('hide');
            } catch (error) {
                this.handleError(error);
            } finally {
                this.formSubmitting = false;
            }
        },
        updateLocalItems(message) {
            if (!message) {
                this.fetchMessages();
                return;
            }
            const index = this.items.findIndex(item => item.id === message.id);
            if (index === -1) {
                this.items.unshift(message);
            } else {
                this.items.splice(index, 1, message);
            }
            this.items.sort((a, b) => new Date(a.schedule_at) - new Date(b.schedule_at));
        },
        async handleDelete(item) {
            if (this.deleteLoading === item.id) {
                return;
            }
            const ok = confirm("Cancel this scheduled message?");
            if (!ok) {
                return;
            }
            this.deleteLoading = item.id;
            try {
                await window.http.delete(`/schedule/messages/${item.id}`);
                this.items = this.items.filter(existing => existing.id !== item.id);
                showSuccessInfo('Scheduled message deleted');
            } catch (error) {
                this.handleError(error);
            } finally {
                this.deleteLoading = null;
            }
        },
        async handleRunNow(item) {
            if (this.actionLoading === item.id) {
                return;
            }
            const ok = confirm("Send this message immediately?");
            if (!ok) {
                return;
            }
            this.actionLoading = item.id;
            try {
                const response = await window.http.post(`/schedule/messages/${item.id}/run`);
                const message = response.data?.results;
                showSuccessInfo('Scheduled message dispatched');
                this.updateLocalItems(message);
            } catch (error) {
                this.handleError(error);
            } finally {
                this.actionLoading = null;
            }
        },
    },
    template: `
    <div class="purple card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui purple right ribbon label">Schedule</a>
            <div class="header">Manage Scheduled Messages</div>
            <div class="description">
                Review, edit, and dispatch scheduled sends
            </div>
        </div>
    </div>

    <!-- Manager Modal -->
    <div class="ui large modal" id="modalScheduleManager">
        <i class="close icon"></i>
        <div class="header">
            Scheduled Messages
        </div>
        <div class="content">
            <div class="ui form">
                <div class="inline fields">
                    <label>Status Filter</label>
                    <div class="field" v-for="option in statusOptions" :key="option.value">
                        <div class="ui checkbox">
                            <input type="checkbox" :value="option.value" v-model="filter_statuses">
                            <label>{{ option.label }}</label>
                        </div>
                    </div>
                    <div class="field">
                        <button type="button" class="ui primary button" :class="{loading: loading}" @click="fetchMessages">
                            <i class="refresh icon"></i> Refresh
                        </button>
                    </div>
                    <div class="field">
                        <button type="button" class="ui primary button" @click="openCreateForm">
                            <i class="plus icon"></i> New Schedule
                        </button>
                    </div>
                </div>
            </div>
            <div class="ui segment" :class="{loading: loading}">
                <table class="ui celled table compact">
                    <thead>
                        <tr>
                            <th>ID</th>
                            <th>Recipient</th>
                            <th>Message</th>
                            <th>Schedule</th>
                            <th>Status</th>
                            <th>Attempts</th>
                            <th>Error</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-if="!loading && items.length === 0">
                            <td colspan="8" class="center aligned">No scheduled messages found</td>
                        </tr>
                        <tr v-for="item in items" :key="item.id">
                            <td>{{ item.id }}</td>
                            <td>
                                <div>{{ formatRecipient(item.phone) }}</div>
                                <div class="meta">{{ item.phone }}</div>
                            </td>
                            <td>{{ shorten(item.message) }}</td>
                            <td>
                                <div>{{ formatTimestamp(item.schedule_at) }}</div>
                                <div v-if="item.sent_at" class="meta">Sent: {{ formatTimestamp(item.sent_at) }}</div>
                            </td>
                            <td v-html="formatStatus(item.status)"></td>
                            <td>{{ item.attempts }}</td>
                            <td>{{ item.error || '—' }}</td>
                            <td>
                                <div class="ui tiny buttons">
                                    <button type="button" class="ui tiny button" @click="openEditForm(item)" :disabled="item.status === 'sent'">
                                        Edit
                                    </button>
                                    <button type="button" class="ui tiny blue button" @click="handleRunNow(item)" v-if="item.status === 'pending'" :class="{loading: actionLoading === item.id}">
                                        Send Now
                                    </button>
                                    <button type="button" class="ui tiny red button" @click="handleDelete(item)" :class="{loading: deleteLoading === item.id}">
                                        Delete
                                    </button>
                                </div>
                            </td>
                        </tr>
                    </tbody>
                </table>
            </div>
        </div>
    </div>

    <!-- Create / Edit Form -->
    <div class="ui small modal" id="modalScheduleForm">
        <i class="close icon"></i>
        <div class="header">
            {{ formMode === 'create' ? 'Create Scheduled Message' : 'Edit Scheduled Message' }}
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="form.type" v-model:phone="form.phone" :show-status="true"/>
                <div class="field">
                    <label>Message</label>
                    <textarea v-model="form.message" aria-label="message" placeholder="Message content"></textarea>
                </div>
                <div class="field" v-if="form.type !== constants.TYPESTATUS">
                    <label>Reply Message ID</label>
                    <input v-model="form.reply_message_id" type="text" placeholder="Optional reply message id" aria-label="reply_message_id">
                </div>
                <div class="field">
                    <div class="ui checkbox">
                        <input type="checkbox" v-model="form.is_forwarded" aria-label="is_forwarded">
                        <label>Mark message as forwarded</label>
                    </div>
                </div>
                <div class="field">
                    <label>Disappearing Duration (seconds)</label>
                    <input v-model="form.duration" type="number" min="0" placeholder="0 (no expiry)" aria-label="duration">
                </div>
                <div class="field">
                    <label>Schedule Time</label>
                    <input v-model="form.schedule_at" type="datetime-local" aria-label="schedule_at">
                    <small>Times use your local timezone.</small>
                </div>
            </form>
        </div>
        <div class="actions">
            <button type="button" class="ui button" @click.prevent="$('#modalScheduleForm').modal('hide')">
                Cancel
            </button>
            <button type="button" class="ui positive right labeled icon button" :class="{disabled: !isValidForm() || formSubmitting, loading: formSubmitting}" @click.prevent="submitForm">
                {{ formMode === 'create' ? 'Create' : 'Save Changes' }}
                <i class="save icon"></i>
            </button>
        </div>
    </div>
    `
}
