export default {
    name: 'WebhookManager',
    data() {
        return {
            loading: false,
            webhooks: [],
            webhookForm: {
                url: '',
                secret: '',
                events: [],
                enabled: true,
                description: ''
            }
        }
    },
    methods: {
        openModal() {
            this.fetchWebhooks()
            $('#modalWebhookManager').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async fetchWebhooks() {
            try {
                const response = await window.http.get('/webhook')
                this.webhooks = response.data.results
            } catch (error) {
                console.error('Failed to fetch webhooks:', error)
                showErrorInfo('Failed to load webhooks')
            }
        },
        isValidForm() {
            if (!this.webhookForm.url || !this.webhookForm.url.trim()) {
                return false
            }
            
            if (this.webhookForm.events.length === 0) {
                return false
            }
            
            return true
        },
        toggleEvent(event) {
            const index = this.webhookForm.events.indexOf(event)
            if (index > -1) {
                this.webhookForm.events.splice(index, 1)
            } else {
                this.webhookForm.events.push(event)
            }
        },
        isEventSelected(event) {
            return this.webhookForm.events.includes(event)
        },
        getSelectedEventsText() {
            if (this.webhookForm.events.length === 0) {
                return 'None'
            }
            return this.webhookForm.events.join(', ')
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                await this.submitApi()
                showSuccessInfo('Webhook created successfully')
                this.handleReset()
                await this.fetchWebhooks()
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                await window.http.post('/webhook', this.webhookForm)
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            } finally {
                this.loading = false;
            }
        },
        handleReset() {
            this.webhookForm = {
                url: '',
                secret: '',
                events: [],
                enabled: true,
                description: ''
            }
        },
        async deleteWebhook(id) {
            if (!confirm('Are you sure you want to delete this webhook?')) {
                return
            }
            
            try {
                await window.http.delete(`/webhook/${id}`)
                showSuccessInfo('Webhook deleted successfully')
                await this.fetchWebhooks()
            } catch (error) {
                showErrorInfo('Failed to delete webhook')
            }
        }
    },
    template: `
    <div class="blue card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Webhook</a>
            <div class="header">Manage Webhooks</div>
            <div class="description">
                Configure webhook endpoints for event notifications
            </div>
        </div>
    </div>
    
    <!-- Webhook Manager Modal -->
    <div class="ui modal" id="modalWebhookManager">
        <i class="close icon"></i>
        <div class="header">
            Manage Webhooks
        </div>
        <div class="scrolling content">
            <div class="ui form">
                <h4 class="ui dividing header">Create Webhook</h4>
                
                <div class="field">
                    <label>Webhook URL</label>
                    <input v-model="webhookForm.url" type="url"
                           placeholder="https://your-webhook-endpoint.com/callback"
                           aria-label="Webhook URL">
                </div>
                
                <div class="field">
                    <label>Secret Key (optional)</label>
                    <input v-model="webhookForm.secret" type="text"
                           placeholder="Secret for HMAC signature"
                           aria-label="Secret Key">
                    <div class="ui pointing label">
                        Leave empty to use default secret
                    </div>
                </div>
                
                <div class="field">
                    <label>Events to Receive</label>
                    <div class="ui six column grid">
                        <div class="column" v-for="event in ['message', 'message.ack', 'group', 'group.join', 'group.leave', 'group.promote', 'group.demote', 'message.delete', 'presence']">
                            <div class="ui checkbox">
                                <input type="checkbox" :id="'event-' + event" 
                                       :value="event" v-model="webhookForm.events">
                                <label :for="'event-' + event">{{ event }}</label>
                            </div>
                        </div>
                    </div>
                    <div class="ui pointing label">
                        Selected events: {{ getSelectedEventsText() }}
                    </div>
                </div>
                
                <div class="field">
                    <div class="ui checkbox">
                        <input type="checkbox" id="webhook-enabled" v-model="webhookForm.enabled">
                        <label for="webhook-enabled">Enabled</label>
                    </div>
                </div>
                
                <div class="field">
                    <label>Description (optional)</label>
                    <textarea v-model="webhookForm.description" 
                              placeholder="Description for this webhook configuration"
                              rows="2" aria-label="Description"></textarea>
                </div>
                
                <button class="ui primary button" :class="{'loading': loading}" 
                        @click="handleSubmit" type="button" :disabled="loading">
                    Create Webhook
                </button>
            </div>
            
            <div class="ui divider"></div>
            
            <h4 class="ui dividing header">Existing Webhooks</h4>
            
            <div class="ui segments" v-if="webhooks.length > 0">
                <div class="ui segment" v-for="webhook in webhooks" :key="webhook.id">
                    <div class="ui grid">
                        <div class="twelve wide column">
                            <div class="ui header">
                                {{ webhook.url }}
                                <div class="sub header">
                                    <span class="ui green label" v-if="webhook.enabled">Enabled</span>
                                    <span class="ui red label" v-else>Disabled</span>
                                    {{ webhook.events.length }} events
                                    <span v-if="webhook.description">- {{ webhook.description }}</span>
                                </div>
                            </div>
                            <div class="ui horizontal list">
                                <span class="item" v-for="event in webhook.events">
                                    <div class="ui mini label">{{ event }}</div>
                                </span>
                            </div>
                        </div>
                        <div class="four wide column right aligned">
                            <button class="ui red icon button" @click="deleteWebhook(webhook.id)">
                                <i class="trash icon"></i>
                            </button>
                        </div>
                    </div>
                </div>
            </div>
            
            <div class="ui message" v-else>
                <div class="header">
                    No webhooks configured
                </div>
                <p>Create your first webhook to start receiving events.</p>
            </div>
        </div>
        <div class="actions">
            <div class="ui black deny button">
                Close
            </div>
        </div>
    </div>
    `
}