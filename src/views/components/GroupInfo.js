export default {
    name: 'GroupInfo',
    components: {},
    data() {
        return {
            group_id: '',
            groupInfo: null,
            loading: false,
        }
    },
    computed: {
        fullGroupID() {
            if (!this.group_id) return '';
            // Ensure suffix
            if (this.group_id.endsWith(window.TYPEGROUP)) {
                return this.group_id;
            }
            return this.group_id + window.TYPEGROUP;
        },
        formattedGroupCreated() {
            if (!this.groupInfo?.GroupCreated) return '';
            return new Date(this.groupInfo.GroupCreated).toLocaleString();
        },
        formattedNameSetAt() {
            if (!this.groupInfo?.NameSetAt) return '';
            return new Date(this.groupInfo.NameSetAt).toLocaleString();
        },
        formattedTopicSetAt() {
            if (!this.groupInfo?.TopicSetAt) return '';
            return new Date(this.groupInfo.TopicSetAt).toLocaleString();
        },
        disappearingTimerText() {
            if (!this.groupInfo?.DisappearingTimer) return '';
            const days = Math.floor(this.groupInfo.DisappearingTimer / (24 * 60 * 60));
            const hours = Math.floor((this.groupInfo.DisappearingTimer % (24 * 60 * 60)) / (60 * 60));
            const minutes = Math.floor((this.groupInfo.DisappearingTimer % (60 * 60)) / 60);
            
            if (days > 0) return `${days} day${days > 1 ? 's' : ''}`;
            if (hours > 0) return `${hours} hour${hours > 1 ? 's' : ''}`;
            if (minutes > 0) return `${minutes} minute${minutes > 1 ? 's' : ''}`;
            return `${this.groupInfo.DisappearingTimer} second${this.groupInfo.DisappearingTimer > 1 ? 's' : ''}`;
        }
    },
    methods: {
        openModal() {
            this.reset();
            $('#modalGroupInfo').modal('show');
        },
        isValidForm() {
            return this.group_id.trim() !== '';
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) return;
            try {
                await this.fetchInfo();
                showSuccessInfo('Group info fetched');
            } catch (err) {
                showErrorInfo(err.message || err);
            }
        },
        async fetchInfo() {
            this.loading = true;
            try {
                const response = await window.http.get('/group/info', {
                    params: { group_id: this.fullGroupID }
                });
                this.groupInfo = response.data.results;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            } finally {
                this.loading = false;
            }
        },
        reset() {
            this.group_id = '';
            this.groupInfo = null;
            this.loading = false;
        },
        formatPhoneNumber(phone) {
            if (!phone) return '';
            return phone.replace('@s.whatsapp.net', '');
        },
        getParticipantRole(participant) {
            if (participant.IsSuperAdmin) return 'Super Admin';
            if (participant.IsAdmin) return 'Admin';
            return 'Member';
        },
        getParticipantRoleColor(participant) {
            if (participant.IsSuperAdmin) return 'red';
            if (participant.IsAdmin) return 'orange';
            return 'blue';
        }
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer;">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">Group Info</div>
            <div class="description">
                Search detailed information about a group by ID
            </div>
        </div>
    </div>

    <!-- Modal -->
    <div class="ui large modal" id="modalGroupInfo">
        <i class="close icon"></i>
        <div class="header">
            <i class="users icon"></i>
            Group Information
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Group ID</label>
                    <div class="ui action input">
                        <input v-model="group_id" placeholder="e.g. 1203630...">
                        <button type="button" class="ui primary button" :class="{'loading': loading, 'disabled': !isValidForm() || loading}" @click.prevent="handleSubmit">
                            <i class="search icon"></i>
                            Search
                        </button>
                    </div>
                    <small class="ui grey text">Full ID: {{ fullGroupID }}</small>
                </div>
            </form>

            <div v-if="groupInfo" style="margin-top: 2rem;">
                <div class="ui stackable two column grid">
                    <!-- Basic Information -->
                    <div class="column">
                        <div class="ui segment">
                            <h3 class="ui header">
                                <i class="info circle icon"></i>
                                <div class="content">
                                    Basic Information
                                </div>
                            </h3>
                            <div class="ui relaxed divided list">
                                <div class="item">
                                    <i class="tag icon"></i>
                                    <div class="content">
                                        <div class="header">Group Name</div>
                                        <div class="description">{{ groupInfo.Name || 'No name' }}</div>
                                    </div>
                                </div>
                                <div class="item">
                                    <i class="id card icon"></i>
                                    <div class="content">
                                        <div class="header">Group ID</div>
                                        <div class="description">{{ groupInfo.JID }}</div>
                                    </div>
                                </div>
                                <div class="item">
                                    <i class="comment icon"></i>
                                    <div class="content">
                                        <div class="header">Description</div>
                                        <div class="description">{{ groupInfo.Topic || 'No description' }}</div>
                                    </div>
                                </div>
                                <div class="item">
                                    <i class="calendar icon"></i>
                                    <div class="content">
                                        <div class="header">Created</div>
                                        <div class="description">{{ formattedGroupCreated }}</div>
                                    </div>
                                </div>
                                <div class="item">
                                    <i class="flag icon"></i>
                                    <div class="content">
                                        <div class="header">Creator Country</div>
                                        <div class="description">{{ groupInfo.CreatorCountryCode }}</div>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>

                    <!-- Group Settings -->
                    <div class="column">
                        <div class="ui segment">
                            <h3 class="ui header">
                                <i class="settings icon"></i>
                                <div class="content">
                                    Group Settings
                                </div>
                            </h3>
                            <div class="ui relaxed list">
                                <div class="item">
                                    <div class="content">
                                        <div class="header">Group Type</div>
                                        <div class="ui labels">
                                            <div class="ui label" :class="groupInfo.IsLocked ? 'red' : 'green'">
                                                <i class="lock icon" v-if="groupInfo.IsLocked"></i>
                                                <i class="unlock icon" v-else></i>
                                                {{ groupInfo.IsLocked ? 'Locked' : 'Unlocked' }}
                                            </div>
                                            <div class="ui label" :class="groupInfo.IsAnnounce ? 'orange' : 'blue'">
                                                <i class="bullhorn icon" v-if="groupInfo.IsAnnounce"></i>
                                                <i class="comments icon" v-else></i>
                                                {{ groupInfo.IsAnnounce ? 'Announcement' : 'Open Chat' }}
                                            </div>
                                        </div>
                                    </div>
                                </div>
                                <div class="item">
                                    <div class="content">
                                        <div class="header">Privacy Settings</div>
                                        <div class="ui labels">
                                            <div class="ui label" :class="groupInfo.IsEphemeral ? 'purple' : 'grey'">
                                                <i class="hourglass icon" v-if="groupInfo.IsEphemeral"></i>
                                                <i class="save icon" v-else></i>
                                                {{ groupInfo.IsEphemeral ? 'Disappearing Messages' : 'Persistent Messages' }}
                                            </div>
                                            <div v-if="groupInfo.IsEphemeral" class="ui purple label">
                                                <i class="clock icon"></i>
                                                {{ disappearingTimerText }}
                                            </div>
                                        </div>
                                    </div>
                                </div>
                                <div class="item">
                                    <div class="content">
                                        <div class="header">Join Settings</div>
                                        <div class="ui labels">
                                            <div class="ui label" :class="groupInfo.IsJoinApprovalRequired ? 'red' : 'green'">
                                                <i class="user check icon" v-if="groupInfo.IsJoinApprovalRequired"></i>
                                                <i class="user plus icon" v-else></i>
                                                {{ groupInfo.IsJoinApprovalRequired ? 'Approval Required' : 'Open Join' }}
                                            </div>
                                            <div class="ui label">
                                                <i class="users icon"></i>
                                                {{ groupInfo.MemberAddMode }}
                                            </div>
                                        </div>
                                    </div>
                                </div>
                                <div class="item">
                                    <div class="content">
                                        <div class="header">Other Settings</div>
                                        <div class="ui labels">
                                            <div v-if="groupInfo.IsIncognito" class="ui grey label">
                                                <i class="user secret icon"></i>
                                                Incognito
                                            </div>
                                            <div v-if="groupInfo.IsParent" class="ui teal label">
                                                <i class="sitemap icon"></i>
                                                Parent Group
                                            </div>
                                            <div v-if="groupInfo.IsDefaultSubGroup" class="ui olive label">
                                                <i class="share icon"></i>
                                                Default Sub Group
                                            </div>
                                        </div>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>

                <!-- Participants -->
                <div class="ui segment">
                    <h3 class="ui header">
                        <i class="users icon"></i>
                        <div class="content">
                            Participants
                            <div class="sub header">{{ groupInfo.Participants ? groupInfo.Participants.length : 0 }} members</div>
                        </div>
                    </h3>
                    <div class="ui relaxed divided list">
                        <div v-for="participant in groupInfo.Participants" :key="participant.JID" class="item">
                            <div class="right floated content">
                                <div class="ui label" :class="getParticipantRoleColor(participant)">
                                    <i class="user icon"></i>
                                    {{ getParticipantRole(participant) }}
                                </div>
                            </div>
                            <i class="user circle icon"></i>
                            <div class="content">
                                <div class="header">{{ formatPhoneNumber(participant.PhoneNumber) }}</div>
                                <div class="description">
                                    JID: {{ participant.JID }}
                                    <span v-if="participant.LID" class="ui grey text"> • LID: {{ participant.LID }}</span>
                                    <span v-if="participant.DisplayName" class="ui grey text"> • {{ participant.DisplayName }}</span>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>

                <!-- Metadata -->
                <div class="ui segment">
                    <h3 class="ui header">
                        <i class="info icon"></i>
                        <div class="content">
                            Metadata
                        </div>
                    </h3>
                    <div class="ui two column stackable grid">
                        <div class="column">
                            <div class="ui relaxed list">
                                <div class="item">
                                    <div class="content">
                                        <div class="header">Name Last Changed</div>
                                        <div class="description">{{ formattedNameSetAt }}</div>
                                        <div class="description">By: {{ formatPhoneNumber(groupInfo.NameSetBy) }}</div>
                                    </div>
                                </div>
                                <div class="item">
                                    <div class="content">
                                        <div class="header">Topic Last Changed</div>
                                        <div class="description">{{ formattedTopicSetAt }}</div>
                                        <div class="description">By: {{ formatPhoneNumber(groupInfo.TopicSetBy) }}</div>
                                    </div>
                                </div>
                            </div>
                        </div>
                        <div class="column">
                            <div class="ui relaxed list">
                                <div class="item">
                                    <div class="content">
                                        <div class="header">Owner</div>
                                        <div class="description">{{ formatPhoneNumber(groupInfo.OwnerJID) }}</div>
                                    </div>
                                </div>
                                <div class="item">
                                    <div class="content">
                                        <div class="header">Participant Version</div>
                                        <div class="description">{{ groupInfo.ParticipantVersionID }}</div>
                                    </div>
                                </div>
                                <div class="item">
                                    <div class="content">
                                        <div class="header">Announce Version</div>
                                        <div class="description">{{ groupInfo.AnnounceVersionID }}</div>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>
    `
}