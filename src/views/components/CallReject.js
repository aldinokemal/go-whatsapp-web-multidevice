export default {
    name: 'CallReject',
    data() {
        return {
            phone: '',
            call_id: '',
            loading: false,
        }
    },
    computed: {
        caller_jid() {
            const trimmed = this.phone.trim().replace(/\s+/g, '');
            if (trimmed.includes('@')) {
                return trimmed;
            }
            return trimmed + window.TYPEUSER;
        }
    },
    methods: {
        openModal() {
            $('#modalCallReject').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isValidForm() {
            if (!this.phone.trim()) {
                return false;
            }
            if (!this.call_id.trim()) {
                return false;
            }
            return true;
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalCallReject').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    caller_jid: this.caller_jid,
                    call_id: this.call_id.trim(),
                }
                let response = await window.http.post(`/call/reject`, payload)
                this.handleReset();
                return response.data.message;
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
            this.phone = '';
            this.call_id = '';
        },
    },
    template: `
    <div class="red card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui red right ribbon label">Call</a>
            <div class="header">Reject Call</div>
            <div class="description">
                 Reject an incoming call by providing the caller JID and call ID from the webhook
            </div>
        </div>
    </div>

    <!--  Modal CallReject  -->
    <div class="ui small modal" id="modalCallReject">
        <i class="close icon"></i>
        <div class="header">
             Reject Incoming Call
        </div>
        <div class="content">
            <form class="ui form" @submit.prevent>
                <div class="field">
                    <label>Caller Phone Number</label>
                    <input v-model="phone" type="text" placeholder="e.g. 628912345678"
                           aria-label="caller phone number">
                    <input :value="caller_jid" disabled aria-label="caller jid">
                </div>

                <div class="field">
                    <label>Call ID</label>
                    <input v-model="call_id" type="text" placeholder="Call ID from webhook event"
                           aria-label="call id">
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve negative right labeled icon button" :class="{'loading': this.loading, 'disabled': !isValidForm() || loading}"
                 @click="handleSubmit">
                Reject Call
                <i class="phone slash icon"></i>
            </button>
        </div>
    </div>
    `
}
