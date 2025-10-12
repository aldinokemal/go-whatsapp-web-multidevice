export default {
    name: 'GroupListParticipants',
    emits: ['closed'],
    data() {
        return {
            selectedGroup: null,
            loading: false,
            participants: [],
        };
    },
    methods: {
        async open(group) {
            if (!group || !group.JID) {
                throw new Error('Invalid group data');
            }

            this.selectedGroup = {
                id: group.JID,
                name: group.Name || this.formatJID(group.JID),
            };
            this.loading = true;
            this.participants = [];

            try {
                const response = await window.http.get(`/group/participants?group_id=${encodeURIComponent(group.JID)}`);
                const results = response.data.results || {};
                this.participants = results.participants || [];
                if (results.name) {
                    this.selectedGroup.name = results.name;
                }
                this.loading = false;
                this.showModal();
            } catch (error) {
                this.loading = false;
                this.selectedGroup = null;

                if (error.response && error.response.data && error.response.data.message) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message || 'Failed to fetch participants');
            }
        },
        close() {
            this.hideModal();
        },
        showModal() {
            if (!this.$refs.modal) return;
            $(this.$refs.modal).modal('show');
        },
        hideModal() {
            if (!this.$refs.modal) return;
            $(this.$refs.modal).modal('hide');
        },
        handleHidden() {
            this.selectedGroup = null;
            this.participants = [];
            this.$emit('closed');
        },
        formatJID(jid) {
            if (!jid) return '';
            return jid.split('@')[0];
        },
        formatParticipantPhone(participant) {
            if (!participant) return '';
            if (participant.phone_number) {
                return this.formatJID(participant.phone_number);
            }
            return this.formatJID(participant.jid);
        },
        getParticipantRole(participant) {
            if (!participant) return 'Member';
            if (participant.is_super_admin) return 'Super Admin';
            if (participant.is_admin) return 'Admin';
            return 'Member';
        },
        getParticipantRoleColor(participant) {
            if (!participant) return 'grey';
            if (participant.is_super_admin) return 'red';
            if (participant.is_admin) return 'orange';
            return 'teal';
        },
    },
    mounted() {
        if (!this.$refs.modal) return;
        $(this.$refs.modal).modal({
            autofocus: false,
            observeChanges: true,
            onHidden: () => {
                this.handleHidden();
            },
        });
    },
    template: `
    <div class="ui modal" ref="modal">
        <i class="close icon"></i>
        <div class="header">
            Group Participants
            <div class="sub header" v-if="selectedGroup && selectedGroup.name">{{ selectedGroup.name }}</div>
        </div>
        <div class="content">
            <div v-if="loading" class="ui active centered inline loader"></div>

            <div v-else-if="!participants.length" class="ui info message">
                <div class="header">No Participants Found</div>
                <p>Unable to retrieve participants for this group.</p>
            </div>

            <table v-else class="ui celled table">
                <thead>
                    <tr>
                        <th>Participant</th>
                        <th>Phone Number</th>
                        <th>LID</th>
                        <th>Role</th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-for="participant in participants" :key="participant.jid">
                        <td>
                            <div class="header">{{ formatJID(participant.jid) }}</div>
                            <div v-if="participant.display_name" class="description">{{ participant.display_name }}</div>
                        </td>
                        <td>
                            {{ formatParticipantPhone(participant) }}
                        </td>
                        <td>
                            <span v-if="participant.lid">{{ formatJID(participant.lid) }}</span>
                            <span v-else>-</span>
                        </td>
                        <td>
                            <div class="ui tiny label" :class="getParticipantRoleColor(participant)">
                                {{ getParticipantRole(participant) }}
                            </div>
                        </td>
                    </tr>
                </tbody>
            </table>
        </div>
        <div class="actions">
            <div class="ui button" @click="close">Close</div>
        </div>
    </div>
    `,
};
